package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kinesis"
	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createKinesisShards(ctx context.Context, awsPort, id string, numShards int) error {
	endpoint := fmt.Sprintf("http://localhost:%v", awsPort)

	client := kinesis.New(session.Must(session.NewSession(&aws.Config{
		Credentials: credentials.NewStaticCredentials("xxxxx", "xxxxx", "xxxxx"),
		Endpoint:    aws.String(endpoint),
		Region:      aws.String("us-east-1"),
	})))

	_, err := client.CreateStreamWithContext(ctx, &kinesis.CreateStreamInput{
		ShardCount: aws.Int64(int64(numShards)),
		StreamName: aws.String("stream-" + id),
	})
	if err != nil {
		return err
	}

	// wait for stream to exist
	return client.WaitUntilStreamExistsWithContext(ctx, &kinesis.DescribeStreamInput{
		StreamName: aws.String("stream-" + id),
	})
}

var _ = registerIntegrationTest("kinesis", func(t *testing.T) {
	// Skip until annoying logs can be removed.
	t.Skip()

	t.Parallel()

	pool, err := dockertest.NewPool("")
	require.NoError(t, err)

	pool.MaxWait = time.Second * 30

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository:   "localstack/localstack",
		ExposedPorts: []string{"4566/tcp"},
		Env:          []string{"SERVICES=dynamodb,kinesis"},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, pool.Purge(resource))
	})

	resource.Expire(900)

	require.NoError(t, pool.Retry(func() error {
		return createKinesisShards(context.Background(), resource.GetPort("4566/tcp"), "testtable", 2)
	}))

	template := `
output:
  kinesis:
    endpoint: http://localhost:$PORT
    region: us-east-1
    stream: stream-$ID
    partition_key: ${! uuid_v4() }
    credentials:
      id: xxxxx
      secret: xxxxx
      token: xxxxx
    batching:
      count: $OUTPUT_BATCH_COUNT

input:
  kinesis_balanced:
    endpoint: http://localhost:$PORT
    stream: stream-$ID
    dynamodb_table: stream-$ID
    start_from_oldest: true
    region: us-east-1
    credentials:
      id: xxxxx
      secret: xxxxx
      token: xxxxx
`
	integrationTests(
		integrationTestOpenClose(),
		// integrationTestMetadata(),
		integrationTestSendBatch(10),
		integrationTestSendBatchCount(10),
		integrationTestStreamSequential(10),
		integrationTestStreamParallel(10),
		integrationTestStreamParallelLossy(10),
		integrationTestStreamParallelLossyThroughReconnect(10),
	).Run(
		t, template,
		testOptPreTest(func(t *testing.T, env *testEnvironment) {
			require.NoError(t, createKinesisShards(env.ctx, resource.GetPort("4566/tcp"), env.configVars.id, 2))
		}),
		testOptPort(resource.GetPort("4566/tcp")),
		testOptAllowDupes(),
	)
})
