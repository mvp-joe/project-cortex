package storage

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestType_Fields verifies Type struct field types match schema.
func TestType_Fields(t *testing.T) {
	t.Parallel()

	typ := Type{
		ID:          "type-123",
		FilePath:    "internal/server/server.go",
		ModulePath:  "internal/server",
		Name:        "Server",
		Kind:        "struct",
		StartLine:   10,
		EndLine:     50,
		IsExported:  true,
		FieldCount:  3,
		MethodCount: 5,
		Fields: []TypeField{
			{ID: "f1", TypeID: "type-123", Name: "config", FieldType: "*Config", Position: 0, IsMethod: false, IsExported: false},
			{ID: "f2", TypeID: "type-123", Name: "logger", FieldType: "*log.Logger", Position: 1, IsMethod: false, IsExported: false},
		},
		Methods: []TypeField{
			{ID: "m1", TypeID: "type-123", Name: "Start", FieldType: "", Position: 0, IsMethod: true, IsExported: true, ParamCount: intPtr(1), ReturnCount: intPtr(1)},
		},
	}

	assert.Equal(t, "type-123", typ.ID)
	assert.Equal(t, "Server", typ.Name)
	assert.Equal(t, "struct", typ.Kind)
	assert.True(t, typ.IsExported)
	assert.Equal(t, 3, typ.FieldCount)
	assert.Equal(t, 5, typ.MethodCount)
	assert.Len(t, typ.Fields, 2)
	assert.Equal(t, "config", typ.Fields[0].Name)
}

// TestType_Kinds verifies different type kinds.
func TestType_Kinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind string
	}{
		{"interface", "interface"},
		{"struct", "struct"},
		{"class", "class"},
		{"enum", "enum"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			typ := Type{
				ID:         "type-" + tt.name,
				FilePath:   "test.go",
				ModulePath: "test",
				Name:       tt.name,
				Kind:       tt.kind,
				StartLine:  1,
				EndLine:    10,
			}

			assert.Equal(t, tt.kind, typ.Kind)
		})
	}
}

// TestType_JSONMarshaling verifies Type can be marshaled/unmarshaled.
func TestType_JSONMarshaling(t *testing.T) {
	t.Parallel()

	original := Type{
		ID:          "type-json",
		FilePath:    "server.go",
		ModulePath:  "main",
		Name:        "Server",
		Kind:        "struct",
		StartLine:   1,
		EndLine:     100,
		IsExported:  true,
		FieldCount:  2,
		MethodCount: 3,
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Unmarshal back
	var decoded Type
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// Verify fields preserved
	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.Name, decoded.Name)
	assert.Equal(t, original.Kind, decoded.Kind)
	assert.Equal(t, original.IsExported, decoded.IsExported)
}

// TestTypeField_StructField verifies struct field representation.
func TestTypeField_StructField(t *testing.T) {
	t.Parallel()

	field := TypeField{
		ID:          "field-123",
		TypeID:      "type-123",
		Name:        "config",
		FieldType:   "*Config",
		Position:    0,
		IsMethod:    false,
		IsExported:  false,
		ParamCount:  nil, // Not applicable for fields
		ReturnCount: nil, // Not applicable for fields
	}

	assert.Equal(t, "config", field.Name)
	assert.Equal(t, "*Config", field.FieldType)
	assert.False(t, field.IsMethod)
	assert.False(t, field.IsExported)
	assert.Nil(t, field.ParamCount)
	assert.Nil(t, field.ReturnCount)
}

// TestTypeField_InterfaceMethod verifies interface method representation.
func TestTypeField_InterfaceMethod(t *testing.T) {
	t.Parallel()

	method := TypeField{
		ID:          "method-123",
		TypeID:      "iface-123",
		Name:        "HandleRequest",
		FieldType:   "func(context.Context, *Request) error",
		Position:    0,
		IsMethod:    true,
		IsExported:  true,
		ParamCount:  intPtr(2),
		ReturnCount: intPtr(1),
	}

	assert.Equal(t, "HandleRequest", method.Name)
	assert.True(t, method.IsMethod)
	assert.True(t, method.IsExported)
	require.NotNil(t, method.ParamCount)
	assert.Equal(t, 2, *method.ParamCount)
	require.NotNil(t, method.ReturnCount)
	assert.Equal(t, 1, *method.ReturnCount)
}

// TestTypeField_EmbeddedField verifies embedded field (empty name).
func TestTypeField_EmbeddedField(t *testing.T) {
	t.Parallel()

	field := TypeField{
		ID:        "embed-123",
		TypeID:    "type-123",
		Name:      "", // Embedded fields have empty names
		FieldType: "sync.RWMutex",
		Position:  0,
		IsMethod:  false,
	}

	assert.Empty(t, field.Name)
	assert.Equal(t, "sync.RWMutex", field.FieldType)
	assert.False(t, field.IsMethod)
}

// TestFunction_Method verifies method representation.
func TestFunction_Method(t *testing.T) {
	t.Parallel()

	receiverTypeID := "type-123"
	receiverTypeName := "*Server"

	fn := Function{
		ID:               "fn-123",
		FilePath:         "server.go",
		ModulePath:       "main",
		Name:             "Start",
		StartLine:        10,
		EndLine:          50,
		LineCount:        40,
		IsExported:       true,
		IsMethod:         true,
		ReceiverTypeID:   &receiverTypeID,
		ReceiverTypeName: &receiverTypeName,
		ParamCount:       1,
		ReturnCount:      1,
		Parameters: []FunctionParameter{
			{ID: "p1", FunctionID: "fn-123", Name: stringPtr("ctx"), ParamType: "context.Context", Position: 0, IsReturn: false},
		},
		ReturnValues: []FunctionParameter{
			{ID: "r1", FunctionID: "fn-123", Name: nil, ParamType: "error", Position: 0, IsReturn: true},
		},
	}

	assert.Equal(t, "Start", fn.Name)
	assert.True(t, fn.IsMethod)
	assert.NotNil(t, fn.ReceiverTypeID)
	assert.Equal(t, "type-123", *fn.ReceiverTypeID)
	assert.NotNil(t, fn.ReceiverTypeName)
	assert.Equal(t, "*Server", *fn.ReceiverTypeName)
	assert.Len(t, fn.Parameters, 1)
}

// TestFunction_StandaloneFunction verifies non-method function.
func TestFunction_StandaloneFunction(t *testing.T) {
	t.Parallel()

	fn := Function{
		ID:               "fn-456",
		FilePath:         "util.go",
		ModulePath:       "util",
		Name:             "ParseConfig",
		StartLine:        5,
		EndLine:          20,
		LineCount:        15,
		IsExported:       true,
		IsMethod:         false,
		ReceiverTypeID:   nil,
		ReceiverTypeName: nil,
		ParamCount:       1,
		ReturnCount:      2,
	}

	assert.False(t, fn.IsMethod)
	assert.Nil(t, fn.ReceiverTypeID)
	assert.Nil(t, fn.ReceiverTypeName)
}

// TestFunction_JSONMarshaling verifies Function serialization.
func TestFunction_JSONMarshaling(t *testing.T) {
	t.Parallel()

	receiverTypeID := "type-123"
	receiverTypeName := "*Server"

	original := Function{
		ID:               "fn-json",
		FilePath:         "server.go",
		ModulePath:       "main",
		Name:             "Process",
		StartLine:        1,
		EndLine:          100,
		LineCount:        99,
		IsExported:       true,
		IsMethod:         true,
		ReceiverTypeID:   &receiverTypeID,
		ReceiverTypeName: &receiverTypeName,
		ParamCount:       2,
		ReturnCount:      1,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Function
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.Name, decoded.Name)
	assert.Equal(t, original.IsMethod, decoded.IsMethod)
	require.NotNil(t, decoded.ReceiverTypeID)
	assert.Equal(t, *original.ReceiverTypeID, *decoded.ReceiverTypeID)
}

// TestFunctionParameter_Named verifies named parameters.
func TestFunctionParameter_Named(t *testing.T) {
	t.Parallel()

	param := FunctionParameter{
		ID:         "param-123",
		FunctionID: "fn-123",
		Name:       stringPtr("request"),
		ParamType:  "*http.Request",
		Position:   0,
		IsReturn:   false,
		IsVariadic: false,
	}

	require.NotNil(t, param.Name)
	assert.Equal(t, "request", *param.Name)
	assert.Equal(t, "*http.Request", param.ParamType)
	assert.False(t, param.IsReturn)
	assert.False(t, param.IsVariadic)
}

// TestFunctionParameter_UnnamedReturn verifies unnamed return values.
func TestFunctionParameter_UnnamedReturn(t *testing.T) {
	t.Parallel()

	returnVal := FunctionParameter{
		ID:         "ret-123",
		FunctionID: "fn-123",
		Name:       nil,
		ParamType:  "error",
		Position:   0,
		IsReturn:   true,
		IsVariadic: false,
	}

	assert.Nil(t, returnVal.Name)
	assert.True(t, returnVal.IsReturn)
	assert.Equal(t, "error", returnVal.ParamType)
}

// TestFunctionParameter_Variadic verifies variadic parameters.
func TestFunctionParameter_Variadic(t *testing.T) {
	t.Parallel()

	param := FunctionParameter{
		ID:         "param-var",
		FunctionID: "fn-var",
		Name:       stringPtr("args"),
		ParamType:  "...interface{}",
		Position:   1,
		IsReturn:   false,
		IsVariadic: true,
	}

	assert.True(t, param.IsVariadic)
	assert.Equal(t, "...interface{}", param.ParamType)
}

// TestImport_External verifies external dependency import.
func TestImport_External(t *testing.T) {
	t.Parallel()

	imp := Import{
		ID:            "imp-123",
		FilePath:      "server.go",
		ImportPath:    "github.com/user/pkg",
		IsStandardLib: false,
		IsExternal:    true,
		IsRelative:    false,
		ImportLine:    5,
	}

	assert.Equal(t, "github.com/user/pkg", imp.ImportPath)
	assert.False(t, imp.IsStandardLib)
	assert.True(t, imp.IsExternal)
	assert.False(t, imp.IsRelative)
}

// TestImport_StandardLib verifies standard library import.
func TestImport_StandardLib(t *testing.T) {
	t.Parallel()

	imp := Import{
		ID:            "imp-std",
		FilePath:      "main.go",
		ImportPath:    "context",
		IsStandardLib: true,
		IsExternal:    false,
		IsRelative:    false,
		ImportLine:    3,
	}

	assert.True(t, imp.IsStandardLib)
	assert.False(t, imp.IsExternal)
	assert.False(t, imp.IsRelative)
}

// TestImport_Relative verifies relative import.
func TestImport_Relative(t *testing.T) {
	t.Parallel()

	imp := Import{
		ID:            "imp-rel",
		FilePath:      "handler.go",
		ImportPath:    "./server",
		IsStandardLib: false,
		IsExternal:    false,
		IsRelative:    true,
		ImportLine:    7,
	}

	assert.True(t, imp.IsRelative)
	assert.False(t, imp.IsExternal)
	assert.False(t, imp.IsStandardLib)
	assert.Equal(t, "./server", imp.ImportPath)
}

// TestTypeRelationship_Implements verifies implements relationship.
func TestTypeRelationship_Implements(t *testing.T) {
	t.Parallel()

	rel := TypeRelationship{
		ID:               "rel-123",
		FromTypeID:       "struct-id",
		ToTypeID:         "interface-id",
		RelationshipType: "implements",
		SourceFilePath:   "server.go",
		SourceLine:       10,
	}

	assert.Equal(t, "implements", rel.RelationshipType)
	assert.Equal(t, "struct-id", rel.FromTypeID)
	assert.Equal(t, "interface-id", rel.ToTypeID)
}

// TestTypeRelationship_Embeds verifies embeds relationship.
func TestTypeRelationship_Embeds(t *testing.T) {
	t.Parallel()

	rel := TypeRelationship{
		ID:               "rel-embed",
		FromTypeID:       "outer-struct",
		ToTypeID:         "inner-struct",
		RelationshipType: "embeds",
		SourceFilePath:   "types.go",
		SourceLine:       15,
	}

	assert.Equal(t, "embeds", rel.RelationshipType)
	assert.Equal(t, "outer-struct", rel.FromTypeID)
	assert.Equal(t, "inner-struct", rel.ToTypeID)
}

// TestTypeRelationship_Extends verifies extends relationship.
func TestTypeRelationship_Extends(t *testing.T) {
	t.Parallel()

	rel := TypeRelationship{
		ID:               "rel-extends",
		FromTypeID:       "child-class",
		ToTypeID:         "parent-class",
		RelationshipType: "extends",
		SourceFilePath:   "classes.py",
		SourceLine:       20,
	}

	assert.Equal(t, "extends", rel.RelationshipType)
}

// TestFunctionCall_InternalCall verifies internal function call.
func TestFunctionCall_InternalCall(t *testing.T) {
	t.Parallel()

	calleeID := "callee-fn-id"
	column := 15

	call := FunctionCall{
		ID:               "call-123",
		CallerFunctionID: "caller-fn-id",
		CalleeFunctionID: &calleeID,
		CalleeName:       "ProcessRequest",
		SourceFilePath:   "handler.go",
		CallLine:         25,
		CallColumn:       &column,
	}

	assert.Equal(t, "caller-fn-id", call.CallerFunctionID)
	assert.NotNil(t, call.CalleeFunctionID)
	assert.Equal(t, "callee-fn-id", *call.CalleeFunctionID)
	assert.Equal(t, "ProcessRequest", call.CalleeName)
	assert.NotNil(t, call.CallColumn)
	assert.Equal(t, 15, *call.CallColumn)
}

// TestFunctionCall_ExternalCall verifies external function call.
func TestFunctionCall_ExternalCall(t *testing.T) {
	t.Parallel()

	call := FunctionCall{
		ID:               "call-ext",
		CallerFunctionID: "caller-fn-id",
		CalleeFunctionID: nil, // External call
		CalleeName:       "fmt.Println",
		SourceFilePath:   "main.go",
		CallLine:         10,
		CallColumn:       nil,
	}

	assert.Nil(t, call.CalleeFunctionID)
	assert.Equal(t, "fmt.Println", call.CalleeName)
	assert.Nil(t, call.CallColumn)
}

// TestAllModels_JSONRoundTrip verifies all models can be marshaled/unmarshaled.
func TestAllModels_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model interface{}
	}{
		{
			name: "Type",
			model: Type{
				ID:         "t-1",
				FilePath:   "test.go",
				ModulePath: "test",
				Name:       "TestType",
				Kind:       "struct",
				StartLine:  1,
				EndLine:    10,
			},
		},
		{
			name: "TypeField",
			model: TypeField{
				ID:        "tf-1",
				TypeID:    "t-1",
				Name:      "field",
				FieldType: "string",
				Position:  0,
			},
		},
		{
			name: "Function",
			model: Function{
				ID:         "fn-1",
				FilePath:   "test.go",
				ModulePath: "test",
				Name:       "TestFunc",
				StartLine:  1,
				EndLine:    10,
				LineCount:  9,
			},
		},
		{
			name: "FunctionParameter",
			model: FunctionParameter{
				ID:         "p-1",
				FunctionID: "fn-1",
				ParamType:  "string",
				Position:   0,
			},
		},
		{
			name: "Import",
			model: Import{
				ID:         "i-1",
				FilePath:   "test.go",
				ImportPath: "fmt",
				ImportLine: 3,
			},
		},
		{
			name: "TypeRelationship",
			model: TypeRelationship{
				ID:               "rel-1",
				FromTypeID:       "t-1",
				ToTypeID:         "t-2",
				RelationshipType: "implements",
				SourceFilePath:   "test.go",
				SourceLine:       1,
			},
		},
		{
			name: "FunctionCall",
			model: FunctionCall{
				ID:               "c-1",
				CallerFunctionID: "fn-1",
				CalleeName:       "OtherFunc",
				SourceFilePath:   "test.go",
				CallLine:         5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Marshal
			data, err := json.Marshal(tt.model)
			require.NoError(t, err, "Marshal should succeed")
			assert.NotEmpty(t, data, "Marshaled data should not be empty")

			// Verify we can unmarshal
			var generic map[string]interface{}
			err = json.Unmarshal(data, &generic)
			require.NoError(t, err, "Unmarshal to map should succeed")
		})
	}
}

// TestFunction_CyclomaticComplexity verifies cyclomatic complexity field.
func TestFunction_CyclomaticComplexity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		complexity *int
	}{
		{
			name:       "function with complexity",
			complexity: intPtr(15),
		},
		{
			name:       "function without complexity",
			complexity: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fn := Function{
				ID:                   "fn-complexity",
				FilePath:             "test.go",
				ModulePath:           "test",
				Name:                 "ComplexFunc",
				StartLine:            1,
				EndLine:              100,
				LineCount:            99,
				CyclomaticComplexity: tt.complexity,
			}

			data, err := json.Marshal(fn)
			require.NoError(t, err)

			var decoded Function
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)

			if tt.complexity == nil {
				assert.Nil(t, decoded.CyclomaticComplexity)
			} else {
				require.NotNil(t, decoded.CyclomaticComplexity)
				assert.Equal(t, *tt.complexity, *decoded.CyclomaticComplexity)
			}
		})
	}
}

// Helper function to create string pointer.
func stringPtr(s string) *string {
	return &s
}

// Helper function to create int pointer.
func intPtr(i int) *int {
	return &i
}
