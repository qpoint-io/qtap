package redis

// RESPType represents the type of a RESP value
type RESPType byte

const (
	TypeSimpleString RESPType = '+'
	TypeError        RESPType = '-'
	TypeInteger      RESPType = ':'
	TypeBulkString   RESPType = '$'
	TypeArray        RESPType = '*'
	// RESP3 types
	TypeNull      RESPType = '_'
	TypeBoolean   RESPType = '#'
	TypeDouble    RESPType = ','
	TypeBigNumber RESPType = '('
	TypeBulkError RESPType = '!'
	TypeVerbatim  RESPType = '='
	TypeMap       RESPType = '%'
	TypeSet       RESPType = '~'
	TypePush      RESPType = '>'
)

// String returns a human-readable name for the RESP type
func (t RESPType) String() string {
	switch t {
	case TypeSimpleString:
		return "simple_string"
	case TypeError:
		return "error"
	case TypeInteger:
		return "integer"
	case TypeBulkString:
		return "bulk_string"
	case TypeArray:
		return "array"
	case TypeNull:
		return "null"
	case TypeBoolean:
		return "boolean"
	case TypeDouble:
		return "double"
	case TypeBigNumber:
		return "big_number"
	case TypeBulkError:
		return "bulk_error"
	case TypeVerbatim:
		return "verbatim"
	case TypeMap:
		return "map"
	case TypeSet:
		return "set"
	case TypePush:
		return "push"
	default:
		return "unknown"
	}
}

// Value represents a parsed RESP value
type Value struct {
	Type   RESPType
	Str    string           // For simple/bulk strings, errors, verbatim
	Int    int64            // For integers
	Bool   bool             // For booleans
	Float  float64          // For doubles
	Array  []Value          // For arrays, sets, pushes
	Map    map[string]Value // For maps
	IsNull bool             // For null values
}

// Command represents a parsed Redis command
type Command struct {
	Name string
	Args []string
}

// ToCommand extracts command name and args from an array value
func (v *Value) ToCommand() (*Command, bool) {
	if v.Type != TypeArray || len(v.Array) == 0 {
		return nil, false
	}

	cmd := &Command{
		Args: make([]string, 0, len(v.Array)-1),
	}

	for i, elem := range v.Array {
		// Commands are arrays of bulk strings
		if elem.Type != TypeBulkString {
			continue
		}
		if i == 0 {
			cmd.Name = elem.Str
		} else {
			cmd.Args = append(cmd.Args, elem.Str)
		}
	}

	if cmd.Name == "" {
		return nil, false
	}

	return cmd, true
}
