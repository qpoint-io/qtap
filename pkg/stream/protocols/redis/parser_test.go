package redis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSimpleString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{
			name:  "ok response",
			input: "+OK\r\n",
			want:  "OK",
		},
		{
			name:  "pong response",
			input: "+PONG\r\n",
			want:  "PONG",
		},
		{
			name:  "empty string",
			input: "+\r\n",
			want:  "",
		},
		{
			name:  "string with spaces",
			input: "+hello world\r\n",
			want:  "hello world",
		},
		{
			name:    "incomplete - no terminator",
			input:   "+OK",
			wantErr: ErrIncomplete,
		},
		{
			name:    "incomplete - partial terminator",
			input:   "+OK\r",
			wantErr: ErrIncomplete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			p.Append([]byte(tt.input))

			val, err := p.Parse()
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, TypeSimpleString, val.Type)
			assert.Equal(t, tt.want, val.Str)
		})
	}
}

func TestParseError(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "generic error",
			input: "-ERR unknown command\r\n",
			want:  "ERR unknown command",
		},
		{
			name:  "wrong type error",
			input: "-WRONGTYPE Operation against a key holding the wrong kind of value\r\n",
			want:  "WRONGTYPE Operation against a key holding the wrong kind of value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			p.Append([]byte(tt.input))

			val, err := p.Parse()
			require.NoError(t, err)
			assert.Equal(t, TypeError, val.Type)
			assert.Equal(t, tt.want, val.Str)
		})
	}
}

func TestParseInteger(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr error
	}{
		{
			name:  "zero",
			input: ":0\r\n",
			want:  0,
		},
		{
			name:  "positive",
			input: ":1000\r\n",
			want:  1000,
		},
		{
			name:  "negative",
			input: ":-42\r\n",
			want:  -42,
		},
		{
			name:  "explicit positive",
			input: ":+123\r\n",
			want:  123,
		},
		{
			name:    "incomplete",
			input:   ":123",
			wantErr: ErrIncomplete,
		},
		{
			name:    "invalid",
			input:   ":abc\r\n",
			wantErr: ErrInvalidFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			p.Append([]byte(tt.input))

			val, err := p.Parse()
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, TypeInteger, val.Type)
			assert.Equal(t, tt.want, val.Int)
		})
	}
}

func TestParseBulkString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantNull bool
		wantErr  error
	}{
		{
			name:  "simple string",
			input: "$5\r\nhello\r\n",
			want:  "hello",
		},
		{
			name:  "empty string",
			input: "$0\r\n\r\n",
			want:  "",
		},
		{
			name:     "null bulk string",
			input:    "$-1\r\n",
			wantNull: true,
		},
		{
			name:  "binary data",
			input: "$4\r\n\x00\x01\x02\x03\r\n",
			want:  "\x00\x01\x02\x03",
		},
		{
			name:  "string with crlf inside",
			input: "$7\r\nhel\r\nlo\r\n",
			want:  "hel\r\nlo",
		},
		{
			name:    "incomplete - no length terminator",
			input:   "$5",
			wantErr: ErrIncomplete,
		},
		{
			name:    "incomplete - partial data",
			input:   "$5\r\nhel",
			wantErr: ErrIncomplete,
		},
		{
			name:    "incomplete - no data terminator",
			input:   "$5\r\nhello",
			wantErr: ErrIncomplete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			p.Append([]byte(tt.input))

			val, err := p.Parse()
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, TypeBulkString, val.Type)
			if tt.wantNull {
				assert.True(t, val.IsNull)
			} else {
				assert.Equal(t, tt.want, val.Str)
			}
		})
	}
}

func TestParseArray(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCount int
		wantNull  bool
		wantErr   error
	}{
		{
			name:      "empty array",
			input:     "*0\r\n",
			wantCount: 0,
		},
		{
			name:      "array of integers",
			input:     "*3\r\n:1\r\n:2\r\n:3\r\n",
			wantCount: 3,
		},
		{
			name:      "array of bulk strings",
			input:     "*2\r\n$5\r\nhello\r\n$5\r\nworld\r\n",
			wantCount: 2,
		},
		{
			name:      "mixed array",
			input:     "*3\r\n:1\r\n$5\r\nhello\r\n+OK\r\n",
			wantCount: 3,
		},
		{
			name:     "null array",
			input:    "*-1\r\n",
			wantNull: true,
		},
		{
			name:    "incomplete - no count terminator",
			input:   "*3",
			wantErr: ErrIncomplete,
		},
		{
			name:    "incomplete - missing elements",
			input:   "*3\r\n:1\r\n:2\r\n",
			wantErr: ErrIncomplete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			p.Append([]byte(tt.input))

			val, err := p.Parse()
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, TypeArray, val.Type)
			if tt.wantNull {
				assert.True(t, val.IsNull)
			} else {
				assert.Len(t, val.Array, tt.wantCount)
			}
		})
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantCmd  string
		wantArgs []string
	}{
		{
			name:     "PING",
			input:    "*1\r\n$4\r\nPING\r\n",
			wantCmd:  "PING",
			wantArgs: []string{},
		},
		{
			name:     "SET",
			input:    "*3\r\n$3\r\nSET\r\n$5\r\nmykey\r\n$7\r\nmyvalue\r\n",
			wantCmd:  "SET",
			wantArgs: []string{"mykey", "myvalue"},
		},
		{
			name:     "GET",
			input:    "*2\r\n$3\r\nGET\r\n$5\r\nmykey\r\n",
			wantCmd:  "GET",
			wantArgs: []string{"mykey"},
		},
		{
			name:     "HSET",
			input:    "*4\r\n$4\r\nHSET\r\n$6\r\nmyhash\r\n$5\r\nfield\r\n$5\r\nvalue\r\n",
			wantCmd:  "HSET",
			wantArgs: []string{"myhash", "field", "value"},
		},
		{
			name:     "LPUSH multiple",
			input:    "*4\r\n$5\r\nLPUSH\r\n$6\r\nmylist\r\n$1\r\na\r\n$1\r\nb\r\n",
			wantCmd:  "LPUSH",
			wantArgs: []string{"mylist", "a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			p.Append([]byte(tt.input))

			val, err := p.Parse()
			require.NoError(t, err)

			cmd, ok := val.ToCommand()
			require.True(t, ok)
			assert.Equal(t, tt.wantCmd, cmd.Name)
			assert.Equal(t, tt.wantArgs, cmd.Args)
		})
	}
}

func TestParseNull(t *testing.T) {
	p := NewParser()
	p.Append([]byte("_\r\n"))

	val, err := p.Parse()
	require.NoError(t, err)
	assert.Equal(t, TypeNull, val.Type)
	assert.True(t, val.IsNull)
}

func TestParseBoolean(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "true",
			input: "#t\r\n",
			want:  true,
		},
		{
			name:  "false",
			input: "#f\r\n",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			p.Append([]byte(tt.input))

			val, err := p.Parse()
			require.NoError(t, err)
			assert.Equal(t, TypeBoolean, val.Type)
			assert.Equal(t, tt.want, val.Bool)
		})
	}
}

func TestParseDouble(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  float64
	}{
		{
			name:  "integer value",
			input: ",10\r\n",
			want:  10.0,
		},
		{
			name:  "decimal value",
			input: ",1.23\r\n",
			want:  1.23,
		},
		{
			name:  "negative",
			input: ",-3.14\r\n",
			want:  -3.14,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			p.Append([]byte(tt.input))

			val, err := p.Parse()
			require.NoError(t, err)
			assert.Equal(t, TypeDouble, val.Type)
			assert.InDelta(t, tt.want, val.Float, 0.001)
		})
	}
}

func TestParseBigNumber(t *testing.T) {
	p := NewParser()
	p.Append([]byte("(3492890328409238509324850943850943825024385\r\n"))

	val, err := p.Parse()
	require.NoError(t, err)
	assert.Equal(t, TypeBigNumber, val.Type)
	assert.Equal(t, "3492890328409238509324850943850943825024385", val.Str)
}

func TestParseBulkError(t *testing.T) {
	p := NewParser()
	p.Append([]byte("!21\r\nSYNTAX invalid syntax\r\n"))

	val, err := p.Parse()
	require.NoError(t, err)
	assert.Equal(t, TypeBulkError, val.Type)
	assert.Equal(t, "SYNTAX invalid syntax", val.Str)
}

func TestParseVerbatim(t *testing.T) {
	p := NewParser()
	p.Append([]byte("=15\r\ntxt:Some string\r\n"))

	val, err := p.Parse()
	require.NoError(t, err)
	assert.Equal(t, TypeVerbatim, val.Type)
	assert.Equal(t, "txt:Some string", val.Str)
}

func TestParseMap(t *testing.T) {
	p := NewParser()
	// Map with 2 entries: {first: 1, second: 2}
	p.Append([]byte("%2\r\n+first\r\n:1\r\n+second\r\n:2\r\n"))

	val, err := p.Parse()
	require.NoError(t, err)
	assert.Equal(t, TypeMap, val.Type)
	assert.Len(t, val.Map, 2)
	assert.Equal(t, int64(1), val.Map["first"].Int)
	assert.Equal(t, int64(2), val.Map["second"].Int)
}

func TestParseSet(t *testing.T) {
	p := NewParser()
	// Set with 3 elements
	p.Append([]byte("~3\r\n+a\r\n+b\r\n+c\r\n"))

	val, err := p.Parse()
	require.NoError(t, err)
	assert.Equal(t, TypeSet, val.Type)
	assert.Len(t, val.Array, 3)
}

func TestParsePush(t *testing.T) {
	p := NewParser()
	// Push with 2 elements
	p.Append([]byte(">2\r\n+pubsub\r\n+message\r\n"))

	val, err := p.Parse()
	require.NoError(t, err)
	assert.Equal(t, TypePush, val.Type)
	assert.Len(t, val.Array, 2)
}

func TestChunkedInput(t *testing.T) {
	p := NewParser()

	// Simulate data arriving in chunks
	chunks := []string{
		"*3\r\n$3",
		"\r\nSET\r\n$5",
		"\r\nmykey\r\n$7",
		"\r\nmyvalue\r\n",
	}

	// First chunks should return incomplete
	for i := range len(chunks) - 1 {
		p.Append([]byte(chunks[i]))
		_, err := p.Parse()
		require.ErrorIs(t, err, ErrIncomplete, "chunk %d should be incomplete", i)
	}

	// Last chunk should complete the message
	p.Append([]byte(chunks[len(chunks)-1]))
	val, err := p.Parse()
	require.NoError(t, err)

	cmd, ok := val.ToCommand()
	require.True(t, ok)
	assert.Equal(t, "SET", cmd.Name)
	assert.Equal(t, []string{"mykey", "myvalue"}, cmd.Args)
}

func TestMultipleMessages(t *testing.T) {
	p := NewParser()

	// Multiple responses in one buffer
	p.Append([]byte("+OK\r\n+PONG\r\n:1000\r\n"))

	// Parse first
	val1, err := p.Parse()
	require.NoError(t, err)
	assert.Equal(t, TypeSimpleString, val1.Type)
	assert.Equal(t, "OK", val1.Str)

	// Parse second
	val2, err := p.Parse()
	require.NoError(t, err)
	assert.Equal(t, TypeSimpleString, val2.Type)
	assert.Equal(t, "PONG", val2.Str)

	// Parse third
	val3, err := p.Parse()
	require.NoError(t, err)
	assert.Equal(t, TypeInteger, val3.Type)
	assert.Equal(t, int64(1000), val3.Int)

	// Buffer should be empty now
	_, err = p.Parse()
	assert.ErrorIs(t, err, ErrIncomplete)
}

func TestNestedArray(t *testing.T) {
	p := NewParser()

	// Array containing another array
	p.Append([]byte("*2\r\n*2\r\n:1\r\n:2\r\n*2\r\n:3\r\n:4\r\n"))

	val, err := p.Parse()
	require.NoError(t, err)
	assert.Equal(t, TypeArray, val.Type)
	assert.Len(t, val.Array, 2)

	// First nested array
	assert.Equal(t, TypeArray, val.Array[0].Type)
	assert.Len(t, val.Array[0].Array, 2)
	assert.Equal(t, int64(1), val.Array[0].Array[0].Int)

	// Second nested array
	assert.Equal(t, TypeArray, val.Array[1].Type)
	assert.Len(t, val.Array[1].Array, 2)
	assert.Equal(t, int64(3), val.Array[1].Array[0].Int)
}

func TestBufferManagement(t *testing.T) {
	p := NewParser()

	// Add some data
	p.Append([]byte("+OK\r\n+PONG\r\n"))
	assert.Equal(t, 12, p.BufferLen())

	// Parse first message
	_, err := p.Parse()
	require.NoError(t, err)
	assert.Equal(t, 7, p.BufferLen()) // "+PONG\r\n" remaining

	// Reset
	p.Reset()
	assert.Equal(t, 0, p.BufferLen())
}

func TestInvalidFormat(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "unknown type prefix",
			input: "X123\r\n",
		},
		{
			name:  "invalid integer",
			input: ":not_a_number\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser()
			p.Append([]byte(tt.input))

			_, err := p.Parse()
			assert.Error(t, err)
		})
	}
}

func TestEmptyBuffer(t *testing.T) {
	p := NewParser()
	_, err := p.Parse()
	assert.ErrorIs(t, err, ErrIncomplete)
}

func TestToCommandNotArray(t *testing.T) {
	val := &Value{
		Type: TypeSimpleString,
		Str:  "OK",
	}

	cmd, ok := val.ToCommand()
	assert.False(t, ok)
	assert.Nil(t, cmd)
}

func TestToCommandEmptyArray(t *testing.T) {
	val := &Value{
		Type:  TypeArray,
		Array: []Value{},
	}

	cmd, ok := val.ToCommand()
	assert.False(t, ok)
	assert.Nil(t, cmd)
}
