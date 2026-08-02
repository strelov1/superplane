package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"maps"
	"net"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/superplanehq/superplane/pkg/core"
	"github.com/superplanehq/superplane/test/support/contexts"
)

// These tests run the component against a real in-process SSH server that
// executes what it receives through /bin/sh, so they exercise the actual
// transport: the command travels in the exec request, the export statements
// travel over the session's stdin channel, and the remote shell has to make
// sense of both. Unit tests can only assert on the strings we build; these
// assert on what a host would actually run.

// startTestSSHServer runs an SSH server that accepts any password and executes
// each exec request through /bin/sh, piping the session's stdin to the shell.
// It returns the host and port to connect to.
func startTestSSHServer(t *testing.T) (string, int) {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err, "generate test host key")

	signer, err := ssh.NewSignerFromKey(privateKey)
	require.NoError(t, err, "build test host key signer")

	config := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "listen")
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveTestSSHConnection(conn, config)
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func serveTestSSHConnection(conn net.Conn, config *ssh.ServerConfig) {
	serverConn, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer serverConn.Close()

	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only sessions are supported")
			continue
		}

		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			return
		}

		go serveTestSSHSession(channel, channelRequests)
	}
}

func serveTestSSHSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()

	for request := range requests {
		if request.Type != "exec" {
			if request.WantReply {
				_ = request.Reply(false, nil)
			}
			continue
		}

		// The exec payload is a length-prefixed string holding the command.
		var payload struct{ Command string }
		if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
			if request.WantReply {
				_ = request.Reply(false, nil)
			}
			return
		}

		if request.WantReply {
			_ = request.Reply(true, nil)
		}

		command := exec.Command("/bin/sh", "-c", payload.Command)
		command.Stdin = channel
		command.Stdout = channel
		command.Stderr = channel.Stderr()

		exitCode := 0
		if err := command.Run(); err != nil {
			var exitError *exec.ExitError
			if errors.As(err, &exitError) {
				exitCode = exitError.ExitCode()
			} else {
				exitCode = 1
			}
		}

		_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(exitCode)}))
		return
	}
}

// capturingExecutionState records what the component emits so a test can
// assert on the command result the remote host actually produced.
type capturingExecutionState struct {
	channel  string
	payloads []any
}

func (s *capturingExecutionState) IsFinished() bool                 { return false }
func (s *capturingExecutionState) SetKV(key, value string) error    { return nil }
func (s *capturingExecutionState) GetKV(key string) (string, error) { return "", nil }
func (s *capturingExecutionState) Emit(channel, payloadType string, payloads []any) error {
	s.channel = channel
	s.payloads = payloads
	return nil
}
func (s *capturingExecutionState) EmitAndContinue(channel, payloadType string, payloads []any) error {
	return s.Emit(channel, payloadType, payloads)
}
func (s *capturingExecutionState) Pass() error                       { return nil }
func (s *capturingExecutionState) Fail(reason, message string) error { return nil }

func (s *capturingExecutionState) result(t *testing.T) *CommandResult {
	t.Helper()
	require.Len(t, s.payloads, 1, "expected exactly one emitted payload")
	result, ok := s.payloads[0].(*CommandResult)
	require.True(t, ok, "emitted payload is not a *CommandResult: %T", s.payloads[0])
	return result
}

// runComponentAgainstTestServer executes the component against the in-process
// server and returns the command result it produced.
func runComponentAgainstTestServer(t *testing.T, host string, port int, nodeConfiguration map[string]any, secrets map[string][]byte) *CommandResult {
	t.Helper()

	// Copied rather than mutated in place: the caller's literal stays readable
	// as the exact configuration under test.
	configuration := map[string]any{"timeout": 30}
	maps.Copy(configuration, nodeConfiguration)

	configuration["host"] = host
	configuration["port"] = port
	configuration["username"] = "tester"
	configuration["authentication"] = map[string]any{
		"authMethod": AuthMethodPassword,
		"password":   map[string]any{"secret": "host", "key": "password"},
	}

	secretValues := map[string][]byte{"host/password": []byte("irrelevant")}
	for key, value := range secrets {
		secretValues[key] = value
	}

	state := &capturingExecutionState{}
	err := (&SSHCommand{}).Execute(core.ExecutionContext{
		Configuration:  configuration,
		Metadata:       &testMetadataContext{},
		Secrets:        &contexts.SecretsContext{Values: secretValues},
		ExecutionState: state,
	})
	require.NoError(t, err, "component execution")

	return state.result(t)
}

func TestSSHIntegration_SecretReachesTheRemoteCommand(t *testing.T) {
	host, port := startTestSSHServer(t)

	t.Run("single-line command reads the secret from the environment", func(t *testing.T) {
		result := runComponentAgainstTestServer(t, host, port, map[string]any{
			"commands": `echo "value=[$DB_PASSWORD]"`,
			"environment": []any{map[string]any{
				"name":        "DB_PASSWORD",
				"valueSource": EnvironmentValueSourceSecret,
				"secret":      map[string]any{"secret": "prod-db", "key": "password"},
			}},
		}, map[string][]byte{"prod-db/password": []byte("s3cret")})

		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, result.Stdout, "value=[s3cret]")
	})

	t.Run("a value with quotes, spaces and expansions arrives verbatim", func(t *testing.T) {
		awkward := "a'b c $PATH $(whoami) `id`"
		result := runComponentAgainstTestServer(t, host, port, map[string]any{
			"commands": `printf '%s' "$TRICKY"`,
			"environment": []any{map[string]any{
				"name":        "TRICKY",
				"valueSource": EnvironmentValueSourceSecret,
				"secret":      map[string]any{"secret": "vault", "key": "tricky"},
			}},
		}, map[string][]byte{"vault/tricky": []byte(awkward)})

		assert.Equal(t, 0, result.ExitCode)
		assert.Equal(t, awkward, result.Stdout)
	})

	t.Run("multi-line script reads the secret", func(t *testing.T) {
		result := runComponentAgainstTestServer(t, host, port, map[string]any{
			"commands": "set -e\necho \"token=[$TOKEN]\"",
			"environment": []any{map[string]any{
				"name":        "TOKEN",
				"valueSource": EnvironmentValueSourceSecret,
				"secret":      map[string]any{"secret": "vault", "key": "token"},
			}},
		}, map[string][]byte{"vault/token": []byte("t0k")})

		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, result.Stdout, "token=[t0k]")
	})

	t.Run("literal and secret variables coexist", func(t *testing.T) {
		result := runComponentAgainstTestServer(t, host, port, map[string]any{
			"commands": `echo "$PUBLIC/$PRIVATE"`,
			"environment": []any{
				map[string]any{"name": "PUBLIC", "value": "prod"},
				map[string]any{
					"name":        "PRIVATE",
					"valueSource": EnvironmentValueSourceSecret,
					"secret":      map[string]any{"secret": "vault", "key": "token"},
				},
			},
		}, map[string][]byte{"vault/token": []byte("t0k")})

		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, result.Stdout, "prod/t0k")
	})

	// The point of the whole design: the value must not be visible to anything
	// inspecting the process table on the host.
	t.Run("the secret is absent from the command line the host receives", func(t *testing.T) {
		result := runComponentAgainstTestServer(t, host, port, map[string]any{
			// $0 is the command string /bin/sh was started with on the server.
			"commands": `echo "argv=[$0]"`,
			"environment": []any{map[string]any{
				"name":        "DB_PASSWORD",
				"valueSource": EnvironmentValueSourceSecret,
				"secret":      map[string]any{"secret": "prod-db", "key": "password"},
			}},
		}, map[string][]byte{"prod-db/password": []byte("s3cret")})

		assert.Equal(t, 0, result.ExitCode)
		assert.NotContains(t, result.Stdout, "s3cret")
		assert.Contains(t, result.Stdout, "argv=[")
	})

	t.Run("a working directory still applies alongside a secret", func(t *testing.T) {
		result := runComponentAgainstTestServer(t, host, port, map[string]any{
			"commands":         `echo "$PWD:[$TOKEN]"`,
			"workingDirectory": "/tmp",
			"environment": []any{map[string]any{
				"name":        "TOKEN",
				"valueSource": EnvironmentValueSourceSecret,
				"secret":      map[string]any{"secret": "vault", "key": "token"},
			}},
		}, map[string][]byte{"vault/token": []byte("t0k")})

		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, result.Stdout, "/tmp:[t0k]")
	})

	t.Run("a command without secrets still runs", func(t *testing.T) {
		result := runComponentAgainstTestServer(t, host, port, map[string]any{
			"commands":    `echo "plain=[$PUBLIC]"`,
			"environment": []any{map[string]any{"name": "PUBLIC", "value": "prod"}},
		}, nil)

		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, result.Stdout, "plain=[prod]")
	})

	t.Run("a missing secret fails before the command runs", func(t *testing.T) {
		state := &capturingExecutionState{}
		err := (&SSHCommand{}).Execute(core.ExecutionContext{
			Configuration: map[string]any{
				"host":     host,
				"port":     port,
				"username": "tester",
				"timeout":  30,
				"commands": "echo hi",
				"authentication": map[string]any{
					"authMethod": AuthMethodPassword,
					"password":   map[string]any{"secret": "host", "key": "password"},
				},
				"environment": []any{map[string]any{
					"name":        "DB_PASSWORD",
					"valueSource": EnvironmentValueSourceSecret,
					"secret":      map[string]any{"secret": "prod-db", "key": "absent"},
				}},
			},
			Metadata:       &testMetadataContext{},
			Secrets:        &contexts.SecretsContext{Values: map[string][]byte{"host/password": []byte("x")}},
			ExecutionState: state,
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "DB_PASSWORD")
		assert.Empty(t, state.payloads, "no command should have reached the host")
	})
}

// Sanity check on the test server itself: it must genuinely execute what it is
// given, otherwise the assertions above would pass against a stub.
func TestSSHIntegration_TestServerExecutesCommands(t *testing.T) {
	host, port := startTestSSHServer(t)

	client := NewClientPassword(host, port, "tester", []byte("irrelevant"))
	result, err := client.ExecuteScript("echo hello && exit 3", strings.NewReader(""), 0)

	require.NoError(t, err)
	assert.Equal(t, 3, result.ExitCode)
	assert.Contains(t, result.Stdout, "hello")
}
