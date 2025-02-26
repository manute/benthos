package integration

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jeffail/benthos/v3/lib/config"
	"github.com/Jeffail/benthos/v3/lib/input"
	"github.com/Jeffail/benthos/v3/lib/log"
	"github.com/Jeffail/benthos/v3/lib/manager"
	"github.com/Jeffail/benthos/v3/lib/message"
	"github.com/Jeffail/benthos/v3/lib/metrics"
	"github.com/Jeffail/benthos/v3/lib/output"
	"github.com/Jeffail/benthos/v3/lib/response"
	"github.com/Jeffail/benthos/v3/lib/types"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type testConfigVars struct {
	// A unique identifier for separating this test configuration from others.
	// Usually used to access a different topic, consumer group, directory, etc.
	id string

	// A port to use in connector URLs. Allowing tests to override this
	// potentially enables tests that check for faulty connections by bridging.
	port string

	// A second port to use in secondary connector URLs.
	portTwo string

	// A third port to use in tertiary connector URLs.
	portThree string

	// A fourth port to use in quarternary connector URLs.
	portFour string

	// Used by batching testers to check the input honours batching fields.
	inputBatchCount int

	// Used by batching testers to check the output honours batching fields.
	outputBatchCount int

	// Used by metadata filter tests to check that filters work.
	outputMetaExcludePrefix string

	// Used by testers to check the max in flight option of outputs.
	maxInFlight int

	// Generic variables.
	var1 string
	var2 string
	var3 string
	var4 string
}

type testEnvironment struct {
	configTemplate string
	configVars     testConfigVars

	preTest func(*testing.T, *testEnvironment)

	timeout time.Duration
	ctx     context.Context
	log     log.Modular
	stats   metrics.Type

	allowDuplicateMessages bool

	// Ugly work arounds for slow connectors.
	sleepAfterInput  time.Duration
	sleepAfterOutput time.Duration
}

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func newTestEnvironment(t *testing.T, confTemplate string) testEnvironment {
	t.Helper()

	u4, err := uuid.NewV4()
	require.NoError(t, err)

	return testEnvironment{
		configTemplate: confTemplate,
		configVars: testConfigVars{
			id:          u4.String(),
			maxInFlight: 1,
		},
		timeout: time.Second * 90,
		ctx:     context.Background(),
		log:     log.Noop(),
		stats:   metrics.Noop(),
	}
}

func (e testEnvironment) RenderConfig() string {
	return strings.NewReplacer(
		"$ID", e.configVars.id,
		"$PORT_TWO", e.configVars.portTwo,
		"$PORT_THREE", e.configVars.portThree,
		"$PORT_FOUR", e.configVars.portFour,
		"$PORT", e.configVars.port,
		"$VAR1", e.configVars.var1,
		"$VAR2", e.configVars.var2,
		"$VAR3", e.configVars.var3,
		"$VAR4", e.configVars.var4,
		"$INPUT_BATCH_COUNT", strconv.Itoa(e.configVars.inputBatchCount),
		"$OUTPUT_BATCH_COUNT", strconv.Itoa(e.configVars.outputBatchCount),
		"$OUTPUT_META_EXCLUDE_PREFIX", e.configVars.outputMetaExcludePrefix,
		"$MAX_IN_FLIGHT", strconv.Itoa(e.configVars.maxInFlight),
	).Replace(e.configTemplate)
}

//------------------------------------------------------------------------------

type testOptFunc func(*testEnvironment)

func testOptTimeout(timeout time.Duration) testOptFunc {
	return func(env *testEnvironment) {
		env.timeout = timeout
	}
}

func testOptAllowDupes() testOptFunc {
	return func(env *testEnvironment) {
		env.allowDuplicateMessages = true
	}
}

func testOptMaxInFlight(n int) testOptFunc {
	return func(env *testEnvironment) {
		env.configVars.maxInFlight = n
	}
}

func testOptLogging(level string) testOptFunc {
	return func(env *testEnvironment) {
		logConf := log.NewConfig()
		logConf.LogLevel = level
		env.log = log.New(os.Stdout, logConf)
	}
}

func testOptPort(port string) testOptFunc {
	return func(env *testEnvironment) {
		env.configVars.port = port
	}
}

func testOptPortTwo(portTwo string) testOptFunc {
	return func(env *testEnvironment) {
		env.configVars.portTwo = portTwo
	}
}

func testOptVarOne(v string) testOptFunc {
	return func(env *testEnvironment) {
		env.configVars.var1 = v
	}
}

func testOptVarTwo(v string) testOptFunc {
	return func(env *testEnvironment) {
		env.configVars.var2 = v
	}
}

func testOptVarThree(v string) testOptFunc {
	return func(env *testEnvironment) {
		env.configVars.var3 = v
	}
}

func testOptSleepAfterInput(t time.Duration) testOptFunc {
	return func(env *testEnvironment) {
		env.sleepAfterInput = t
	}
}

func testOptSleepAfterOutput(t time.Duration) testOptFunc {
	return func(env *testEnvironment) {
		env.sleepAfterOutput = t
	}
}

func testOptPreTest(fn func(*testing.T, *testEnvironment)) testOptFunc {
	return func(env *testEnvironment) {
		env.preTest = fn
	}
}

//------------------------------------------------------------------------------

type testDefinition func(*testing.T, *testEnvironment)

type integrationTestList []testDefinition

func integrationTests(tests ...testDefinition) integrationTestList {
	return tests
}

func (i integrationTestList) Run(t *testing.T, configTemplate string, opts ...testOptFunc) {
	envs := make([]testEnvironment, len(i))

	wg := sync.WaitGroup{}
	for j := range i {
		envs[j] = newTestEnvironment(t, configTemplate)
		for _, opt := range opts {
			opt(&envs[j])
		}

		timeout := envs[j].timeout
		if deadline, ok := t.Deadline(); ok {
			timeout = time.Until(deadline) - (time.Second * 5)
		}

		var done func()
		envs[j].ctx, done = context.WithTimeout(envs[j].ctx, timeout)
		t.Cleanup(done)

		if envs[j].preTest != nil {
			wg.Add(1)
			env := &envs[j]
			go func() {
				defer wg.Done()
				env.preTest(t, env)
			}()
		}
	}
	wg.Wait()

	for j, test := range i {
		if envs[j].configVars.port == "" {
			p, err := getFreePort()
			if err != nil {
				t.Fatal(err)
			}
			envs[j].configVars.port = strconv.Itoa(p)
		}
		test(t, &envs[j])
	}
}

func (i integrationTestList) RunSequentially(t *testing.T, configTemplate string, opts ...testOptFunc) {
	for _, test := range i {
		env := newTestEnvironment(t, configTemplate)
		for _, opt := range opts {
			opt(&env)
		}

		timeout := env.timeout
		if deadline, ok := t.Deadline(); ok {
			timeout = time.Until(deadline) - (time.Second * 5)
		}

		var done func()
		env.ctx, done = context.WithTimeout(env.ctx, timeout)
		t.Cleanup(done)

		if env.preTest != nil {
			env.preTest(t, &env)
		}
		t.Run("seq", func(t *testing.T) {
			test(t, &env)
		})
	}
}

var registeredIntegrationTests = map[string]func(*testing.T){}

// register an integration test that should only execute under the `integration`
// build tag. Returns an empty struct so that it can be called at a file root.
func registerIntegrationTest(name string, fn func(*testing.T)) struct{} {
	if _, exists := registeredIntegrationTests[name]; exists {
		panic(fmt.Sprintf("integration test double registered: %v", name))
	}
	registeredIntegrationTests[name] = fn
	return struct{}{}
}

//------------------------------------------------------------------------------

func namedTest(name string, test testDefinition) testDefinition {
	return func(t *testing.T, env *testEnvironment) {
		t.Run(name, func(t *testing.T) {
			test(t, env)
		})
	}
}

//------------------------------------------------------------------------------

func initConnectors(
	t *testing.T,
	trans <-chan types.Transaction,
	env *testEnvironment,
) (types.Input, types.Output) {
	t.Helper()

	out := initOutput(t, trans, env)
	in := initInput(t, env)
	return in, out
}

func initInput(t *testing.T, env *testEnvironment) types.Input {
	t.Helper()

	confBytes := []byte(env.RenderConfig())

	s := config.New()
	dec := yaml.NewDecoder(bytes.NewReader(confBytes))
	dec.KnownFields(true)
	require.NoError(t, dec.Decode(&s))

	lints, err := config.Lint(confBytes, s)
	require.NoError(t, err)
	assert.Empty(t, lints)

	mgr, err := manager.NewV2(s.ResourceConfig, nil, env.log, env.stats)
	require.NoError(t, err)

	input, err := input.New(s.Input, mgr, env.log, env.stats)
	require.NoError(t, err)

	if env.sleepAfterInput > 0 {
		time.Sleep(env.sleepAfterInput)
	}

	return input
}

func initOutput(t *testing.T, trans <-chan types.Transaction, env *testEnvironment) types.Output {
	t.Helper()

	confBytes := []byte(env.RenderConfig())

	s := config.New()
	dec := yaml.NewDecoder(bytes.NewReader(confBytes))
	dec.KnownFields(true)
	require.NoError(t, dec.Decode(&s))

	lints, err := config.Lint(confBytes, s)
	require.NoError(t, err)
	assert.Empty(t, lints)

	mgr, err := manager.NewV2(s.ResourceConfig, nil, env.log, env.stats)
	require.NoError(t, err)

	output, err := output.New(s.Output, mgr, env.log, env.stats)
	require.NoError(t, err)

	require.NoError(t, output.Consume(trans))

	require.Error(t, output.WaitForClose(time.Millisecond*100))
	if env.sleepAfterOutput > 0 {
		time.Sleep(env.sleepAfterOutput)
	}

	return output
}

func closeConnectors(t *testing.T, input types.Input, output types.Output) {
	if output != nil {
		output.CloseAsync()
		require.NoError(t, output.WaitForClose(time.Second*10))
	}
	if input != nil {
		input.CloseAsync()
		require.NoError(t, input.WaitForClose(time.Second*10))
	}
}

func sendMessage(
	ctx context.Context,
	t *testing.T,
	tranChan chan types.Transaction,
	content string,
	metadata ...string,
) error {
	t.Helper()

	p := message.NewPart([]byte(content))
	for i := 0; i < len(metadata); i += 2 {
		p.Metadata().Set(metadata[i], metadata[i+1])
	}
	msg := message.New(nil)
	msg.Append(p)

	resChan := make(chan types.Response)

	select {
	case tranChan <- types.NewTransaction(msg, resChan):
	case <-ctx.Done():
		t.Fatal("timed out on send")
	}

	select {
	case res := <-resChan:
		return res.Error()
	case <-ctx.Done():
	}
	t.Fatal("timed out on response")
	return nil
}

func sendBatch(
	ctx context.Context,
	t *testing.T,
	tranChan chan types.Transaction,
	content []string,
) error {
	t.Helper()

	msg := message.New(nil)
	for _, payload := range content {
		msg.Append(message.NewPart([]byte(payload)))
	}

	resChan := make(chan types.Response)

	select {
	case tranChan <- types.NewTransaction(msg, resChan):
	case <-ctx.Done():
		t.Fatal("timed out on send")
	}

	select {
	case res := <-resChan:
		return res.Error()
	case <-ctx.Done():
	}

	t.Fatal("timed out on response")
	return nil
}

func receiveMessage(
	ctx context.Context,
	t *testing.T,
	tranChan <-chan types.Transaction,
	err error,
) types.Part {
	t.Helper()

	b, resChan := receiveMessageNoRes(ctx, t, tranChan)
	sendResponse(ctx, t, resChan, err)
	return b
}

func sendResponse(ctx context.Context, t *testing.T, resChan chan<- types.Response, err error) {
	var res types.Response = response.NewAck()
	if err != nil {
		res = response.NewError(err)
	}

	select {
	case resChan <- res:
	case <-ctx.Done():
		t.Fatal("timed out on response")
	}
}

// nolint:gocritic // Ignore unnamedResult false positive
func receiveMessageNoRes(ctx context.Context, t *testing.T, tranChan <-chan types.Transaction) (types.Part, chan<- types.Response) {
	t.Helper()

	var tran types.Transaction
	var open bool
	select {
	case tran, open = <-tranChan:
	case <-ctx.Done():
		t.Fatal("timed out on receive")
	}

	require.True(t, open)
	require.Equal(t, tran.Payload.Len(), 1)

	return tran.Payload.Get(0), tran.ResponseChan
}

func messageMatch(t *testing.T, p types.Part, content string, metadata ...string) {
	t.Helper()

	assert.Equal(t, content, string(p.Get()))

	allMetadata := map[string]string{}
	p.Metadata().Iter(func(k, v string) error {
		allMetadata[k] = v
		return nil
	})

	for i := 0; i < len(metadata); i += 2 {
		assert.Equal(t, metadata[i+1], p.Metadata().Get(metadata[i]), fmt.Sprintf("metadata: %v", allMetadata))
	}
}

func messageInSet(t *testing.T, pop, allowDupes bool, p types.Part, set map[string][]string) {
	t.Helper()

	metadata, exists := set[string(p.Get())]
	if allowDupes && !exists {
		return
	}
	require.True(t, exists, "in set: %v, set: %v", string(p.Get()), set)

	for i := 0; i < len(metadata); i += 2 {
		assert.Equal(t, metadata[i+1], p.Metadata().Get(metadata[i]))
	}

	if pop {
		delete(set, string(p.Get()))
	}
}
