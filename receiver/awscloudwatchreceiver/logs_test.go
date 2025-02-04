// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package awscloudwatchreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awscloudwatchreceiver"

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/comparetest"
)

func TestStart(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Region = "us-west-1"
	cfg.Logs.Groups.AutodiscoverConfig = nil

	sink := &consumertest.LogsSink{}
	logsRcvr := newLogsReceiver(cfg, zap.NewNop(), sink)

	err := logsRcvr.Start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	err = logsRcvr.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestPrefixedConfig(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Region = "us-west-1"
	cfg.Logs.PollInterval = 1 * time.Second
	cfg.Logs.Groups = GroupConfig{
		NamedConfigs: map[string]StreamConfig{
			testLogGroupName: {
				Names: []*string{&testLogStreamName},
			},
		},
	}

	sink := &consumertest.LogsSink{}
	alertRcvr := newLogsReceiver(cfg, zap.NewNop(), sink)
	alertRcvr.client = defaultMockClient()

	err := alertRcvr.Start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 2*time.Second, 10*time.Millisecond)

	err = alertRcvr.Shutdown(context.Background())
	require.NoError(t, err)

	logs := sink.AllLogs()[0]
	expected, err := readLogs(filepath.Join("testdata", "processed", "prefixed.json"))
	require.NoError(t, err)
	require.NoError(t, comparetest.CompareLogs(expected, logs, comparetest.IgnoreObservedTimestamp()))
}

func TestPrefixedNamedStreamsConfig(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Region = "us-west-1"
	cfg.Logs.PollInterval = 1 * time.Second
	cfg.Logs.Groups = GroupConfig{
		NamedConfigs: map[string]StreamConfig{
			testLogGroupName: {
				Prefixes: []*string{&testLogStreamPrefix},
			},
		},
	}

	sink := &consumertest.LogsSink{}
	alertRcvr := newLogsReceiver(cfg, zap.NewNop(), sink)
	alertRcvr.client = defaultMockClient()

	err := alertRcvr.Start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 2*time.Second, 10*time.Millisecond)

	err = alertRcvr.Shutdown(context.Background())
	require.NoError(t, err)

	logs := sink.AllLogs()[0]
	expected, err := readLogs(filepath.Join("testdata", "processed", "prefixed.json"))
	require.NoError(t, err)
	require.NoError(t, comparetest.CompareLogs(expected, logs, comparetest.IgnoreObservedTimestamp()))
}

func TestDiscovery(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Region = "us-west-1"
	cfg.Logs.PollInterval = 1 * time.Second
	cfg.Logs.Groups = GroupConfig{
		AutodiscoverConfig: &AutodiscoverConfig{
			Limit: 1,
			Streams: StreamConfig{
				Prefixes: []*string{&testLogStreamPrefix},
				Names:    []*string{&testLogStreamMessage},
			},
		},
	}

	sink := &consumertest.LogsSink{}
	logsRcvr := newLogsReceiver(cfg, zap.NewNop(), sink)
	logsRcvr.client = defaultMockClient()

	require.NoError(t, logsRcvr.Start(context.Background(), componenttest.NewNopHost()))
	require.Eventually(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, len(logsRcvr.groupRequests), 2)
	require.NoError(t, logsRcvr.Shutdown(context.Background()))
}

// Test to ensure that mid collection while streaming results we will
// return early if Shutdown is called
func TestShutdownWhileCollecting(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Region = "us-west-1"
	cfg.Logs.PollInterval = 1 * time.Second
	cfg.Logs.Groups = GroupConfig{
		NamedConfigs: map[string]StreamConfig{
			testLogGroupName: {
				Names: []*string{&testLogStreamName},
			},
		},
	}

	sink := &consumertest.LogsSink{}
	alertRcvr := newLogsReceiver(cfg, zap.NewNop(), sink)
	doneChan := make(chan time.Time, 1)
	mc := &mockClient{}
	mc.On("FilterLogEventsWithContext", mock.Anything, mock.Anything, mock.Anything).Return(&cloudwatchlogs.FilterLogEventsOutput{
		Events:    []*cloudwatchlogs.FilteredLogEvent{},
		NextToken: aws.String("next"),
	}, nil).
		WaitUntil(doneChan)
	alertRcvr.client = mc

	err := alertRcvr.Start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	require.Never(t, func() bool {
		return sink.LogRecordCount() > 0
	}, 3*time.Second, 10*time.Millisecond)

	close(doneChan)
	require.NoError(t, alertRcvr.Shutdown(context.Background()))
}

func defaultMockClient() client {
	mc := &mockClient{}
	mc.On("DescribeLogGroupsWithContext", mock.Anything, mock.Anything, mock.Anything).Return(
		&cloudwatchlogs.DescribeLogGroupsOutput{
			LogGroups: []*cloudwatchlogs.LogGroup{
				{
					LogGroupName: &testLogGroupName,
				},
			},
			NextToken: nil,
		}, nil)
	mc.On("FilterLogEventsWithContext", mock.Anything, mock.Anything, mock.Anything).Return(
		&cloudwatchlogs.FilterLogEventsOutput{
			Events: []*cloudwatchlogs.FilteredLogEvent{
				{
					EventId:       &testEventID,
					IngestionTime: aws.Int64(testIngestionTime),
					LogStreamName: aws.String(testLogStreamName),
					Message:       aws.String(testLogStreamMessage),
					Timestamp:     aws.Int64(testTimeStamp),
				},
			},
			NextToken: nil,
		}, nil)
	return mc
}

var (
	testLogGroupName     = "test-log-group-name"
	testLogStreamName    = "test-log-stream-name"
	testLogStreamPrefix  = "test-log-stream"
	testEventID          = "37134448277055698880077365577645869800162629528367333379"
	testIngestionTime    = int64(1665166252124)
	testTimeStamp        = int64(1665166251014)
	testLogStreamMessage = `"time=\"2022-10-07T18:10:46Z\" level=info msg=\"access granted\" arn=\"arn:aws:iam::892146088969:role/AWSWesleyClusterManagerLambda-NodeManagerRole-16UPVDKA1KBGI\" client=\"127.0.0.1:50252\" groups=\"[]\" method=POST path=/authenticate uid=\"aws-iam-authenticator:892146088969:AROA47OAM7QE2NWPDFDCW\" username=\"eks:node-manager\""`
)

type mockClient struct {
	mock.Mock
}

func (mc *mockClient) DescribeLogGroupsWithContext(ctx context.Context, input *cloudwatchlogs.DescribeLogGroupsInput, opts ...request.Option) (*cloudwatchlogs.DescribeLogGroupsOutput, error) {
	args := mc.Called(ctx, input, opts)
	return args.Get(0).(*cloudwatchlogs.DescribeLogGroupsOutput), args.Error(1)
}

func (mc *mockClient) FilterLogEventsWithContext(ctx context.Context, input *cloudwatchlogs.FilterLogEventsInput, opts ...request.Option) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	args := mc.Called(ctx, input, opts)
	return args.Get(0).(*cloudwatchlogs.FilterLogEventsOutput), args.Error(1)
}

func readLogs(path string) (plog.Logs, error) {
	f, err := os.Open(path)
	if err != nil {
		return plog.Logs{}, err
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return plog.Logs{}, err
	}

	unmarshaler := plog.JSONUnmarshaler{}
	return unmarshaler.UnmarshalLogs(b)
}
