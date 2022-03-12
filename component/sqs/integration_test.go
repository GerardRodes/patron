//go:build integration
// +build integration

package sqs

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	patronsqsclient "github.com/beatlabs/patron/client/sqs/v2"
	"github.com/beatlabs/patron/correlation"
	"github.com/beatlabs/patron/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	region   = "eu-west-1"
	endpoint = "http://localhost:4566"
)

type testMessage struct {
	ID string `json:"id"`
}

func Test_SQS_Consume(t *testing.T) {
	t.Cleanup(func() { mtr.Reset() })

	const queueName = "test-sqs-consume"
	const correlationID = "123"

	api, err := test.CreateSQSAPI(region, endpoint)
	require.NoError(t, err)
	queue, err := test.CreateSQSQueue(api, queueName)
	require.NoError(t, err)

	sent := sendMessage(t, api, correlationID, queue, "1", "2", "3")
	mtr.Reset()

	chReceived := make(chan []*testMessage)
	received := make([]*testMessage, 0)
	count := 0

	procFunc := func(ctx context.Context, b Batch) {
		if ctx.Err() != nil {
			return
		}

		for _, msg := range b.Messages() {
			var m1 testMessage
			require.NoError(t, json.Unmarshal(msg.Body(), &m1))
			received = append(received, &m1)
			require.NoError(t, msg.ACK())
		}

		count += len(b.Messages())
		if count == len(sent) {
			chReceived <- received
		}
	}

	cmp, err := New("123", queueName, api, procFunc, MaxMessages(10),
		PollWaitSeconds(20), VisibilityTimeout(30))
	require.NoError(t, err)

	go func() { require.NoError(t, cmp.Run(context.Background())) }()

	got := <-chReceived

	assert.ElementsMatch(t, sent, got)
	assert.Len(t, mtr.FinishedSpans(), 3)
}

func sendMessage(t *testing.T, api sqsiface.SQSAPI, correlationID, queue string, ids ...string) []*testMessage {
	pub, err := patronsqsclient.New(api)
	require.NoError(t, err)

	ctx := correlation.ContextWithID(context.Background(), correlationID)

	sentMessages := make([]*testMessage, 0, len(ids))

	for _, id := range ids {
		sentMsg := &testMessage{
			ID: id,
		}
		sentMsgBody, err := json.Marshal(sentMsg)
		require.NoError(t, err)

		msg := &sqs.SendMessageInput{
			DelaySeconds: aws.Int64(1),
			MessageBody:  aws.String(string(sentMsgBody)),
			QueueUrl:     aws.String(queue),
		}

		msgID, err := pub.Publish(ctx, msg)
		assert.NoError(t, err)
		assert.NotEmpty(t, msgID)

		sentMessages = append(sentMessages, sentMsg)
	}

	return sentMessages
}
