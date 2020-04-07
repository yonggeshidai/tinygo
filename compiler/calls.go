package compiler

import (
	"go/types"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// For a description of the calling convention in prose, see:
// https://tinygo.org/compiler-internals/calling-convention/

// The maximum number of arguments that can be expanded from a single struct. If
// a struct contains more fields, it is passed as a struct without expanding.
const MaxFieldsPerParam = 3

// paramFlags identifies parameter attributes for flags. Most importantly, it
// determines which parameters are dereferenceable_or_null and which aren't.
type paramFlags uint8

const (
	// Parameter may have the deferenceable_or_null attribute. This attribute
	// cannot be applied to unsafe.Pointer and to the data pointer of slices.
	paramIsDeferenceableOrNull = 1 << iota
)

// createCall creates a new call to runtime.<fnName> with the given arguments.
func (b *builder) createRuntimeCall(fnName string, args []llvm.Value, name string) llvm.Value {
	fn := b.ir.Program.ImportedPackage("runtime").Members[fnName].(*ssa.Function)
	llvmFn := b.getFunction(fn)
	if llvmFn.IsNil() {
		panic("trying to call non-existent function: " + fn.RelString(nil))
	}
	args = append(args, llvm.Undef(b.i8ptrType))            // unused context parameter
	args = append(args, llvm.ConstPointerNull(b.i8ptrType)) // coroutine handle
	return b.createCall(llvmFn, args, name)
}

// createCall creates a call to the given function with the arguments possibly
// expanded.
func (b *builder) createCall(fn llvm.Value, args []llvm.Value, name string) llvm.Value {
	expanded := make([]llvm.Value, 0, len(args))
	for _, arg := range args {
		fragments := b.expandFormalParam(arg)
		expanded = append(expanded, fragments...)
	}
	return b.CreateCall(fn, expanded, name)
}

// Expand an argument type to a list that can be used in a function call
// parameter list.
func expandFormalParamType(t llvm.Type, goType types.Type) ([]llvm.Type, []paramFlags) {
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		fields, fieldFlags := flattenAggregateType(t, goType)
		if len(fields) <= MaxFieldsPerParam {
			return fields, fieldFlags
		} else {
			// failed to lower
			return []llvm.Type{t}, []paramFlags{getTypeFlags(goType)}
		}
	default:
		// TODO: split small arrays
		return []llvm.Type{t}, []paramFlags{getTypeFlags(goType)}
	}
}

// expandFormalParamOffsets returns a list of offsets from the start of an
// object of type t after it would have been split up by expandFormalParam. This
// is useful for debug information, where it is necessary to know the offset
// from the start of the combined object.
func (b *builder) expandFormalParamOffsets(t llvm.Type) []uint64 {
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		fields := b.flattenAggregateTypeOffsets(t)
		if len(fields) <= MaxFieldsPerParam {
			return fields
		} else {
			// failed to lower
			return []uint64{0}
		}
	default:
		// TODO: split small arrays
		return []uint64{0}
	}
}

// expandFormalParam splits a formal param value into pieces, so it can be
// passed directly as part of a function call. For example, it splits up small
// structs into individual fields. It is the equivalent of expandFormalParamType
// for parameter values.
func (b *builder) expandFormalParam(v llvm.Value) []llvm.Value {
	switch v.Type().TypeKind() {
	case llvm.StructTypeKind:
		fieldTypes, _ := flattenAggregateType(v.Type(), nil)
		if len(fieldTypes) <= MaxFieldsPerParam {
			fields := b.flattenAggregate(v)
			if len(fields) != len(fieldTypes) {
				panic("type and value param lowering don't match")
			}
			return fields
		} else {
			// failed to lower
			return []llvm.Value{v}
		}
	default:
		// TODO: split small arrays
		return []llvm.Value{v}
	}
}

// Try to flatten a struct type to a list of types. Returns a 1-element slice
// with the passed in type if this is not possible.
func flattenAggregateType(t llvm.Type, goType types.Type) ([]llvm.Type, []paramFlags) {
	typeFlags := getTypeFlags(goType)
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		fields := make([]llvm.Type, 0, t.StructElementTypesCount())
		fieldFlags := make([]paramFlags, 0, cap(fields))
		for i, subfield := range t.StructElementTypes() {
			subfields, subfieldFlags := flattenAggregateType(subfield, extractSubfield(goType, i))
			for i := range subfieldFlags {
				subfieldFlags[i] |= typeFlags
			}
			fields = append(fields, subfields...)
			fieldFlags = append(fieldFlags, subfieldFlags...)
		}
		return fields, fieldFlags
	default:
		return []llvm.Type{t}, []paramFlags{typeFlags}
	}
}

// getTypeFlags returns the type flags for a given type. It will not recurse
// into sub-types (such as in structs).
func getTypeFlags(t types.Type) paramFlags {
	if t == nil {
		return 0
	}
	switch t.Underlying().(type) {
	case *types.Pointer:
		// Pointers in Go must either point to an object or be nil.
		return paramIsDeferenceableOrNull
	case *types.Chan, *types.Map:
		// Channels and maps are implemented as pointers pointing to some
		// object, and follow the same rules as *types.Pointer.
		return paramIsDeferenceableOrNull
	default:
		return 0
	}
}

// extractSubfield extracts a field from a struct, or returns null if this is
// not a struct and thus no subfield can be obtained.
func extractSubfield(t types.Type, field int) types.Type {
	if t == nil {
		return nil
	}
	switch t := t.Underlying().(type) {
	case *types.Struct:
		return t.Field(field).Type()
	case *types.Interface, *types.Slice, *types.Basic, *types.Signature:
		// These Go types are (sometimes) implemented as LLVM structs but can't
		// really be split further up in Go (with the possible exception of
		// complex numbers).
		return nil
	default:
		// This should be unreachable.
		panic("cannot split subfield: " + t.String())
	}
}

// flattenAggregateTypeOffset returns the offsets from the start of an object of
// type t if this object were flattened like in flattenAggregate. Used together
// with flattenAggregate to know the start indices of each value in the
// non-flattened object.
//
// Note: this is an implementation detail, use expandFormalParamOffsets instead.
func (c *compilerContext) flattenAggregateTypeOffsets(t llvm.Type) []uint64 {
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		fields := make([]uint64, 0, t.StructElementTypesCount())
		for fieldIndex, field := range t.StructElementTypes() {
			suboffsets := c.flattenAggregateTypeOffsets(field)
			offset := c.targetData.ElementOffset(t, fieldIndex)
			for i := range suboffsets {
				suboffsets[i] += offset
			}
			fields = append(fields, suboffsets...)
		}
		return fields
	default:
		return []uint64{0}
	}
}

// flattenAggregate breaks down a struct into its elementary values for argument
// passing. It is the value equivalent of flattenAggregateType
func (b *builder) flattenAggregate(v llvm.Value) []llvm.Value {
	switch v.Type().TypeKind() {
	case llvm.StructTypeKind:
		fields := make([]llvm.Value, 0, v.Type().StructElementTypesCount())
		for i := range v.Type().StructElementTypes() {
			subfield := b.CreateExtractValue(v, i, "")
			subfields := b.flattenAggregate(subfield)
			fields = append(fields, subfields...)
		}
		return fields
	default:
		return []llvm.Value{v}
	}
}

// collapseFormalParam combines an aggregate object back into the original
// value. This is used to join multiple LLVM parameters into a single Go value
// in the function entry block.
func (b *builder) collapseFormalParam(t llvm.Type, fields []llvm.Value) llvm.Value {
	param, remaining := b.collapseFormalParamInternal(t, fields)
	if len(remaining) != 0 {
		panic("failed to expand back all fields")
	}
	return param
}

// collapseFormalParamInternal is an implementation detail of
// collapseFormalParam: it works by recursing until there are no fields left.
func (b *builder) collapseFormalParamInternal(t llvm.Type, fields []llvm.Value) (llvm.Value, []llvm.Value) {
	switch t.TypeKind() {
	case llvm.StructTypeKind:
		flattened, _ := flattenAggregateType(t, nil)
		if len(flattened) <= MaxFieldsPerParam {
			value := llvm.ConstNull(t)
			for i, subtyp := range t.StructElementTypes() {
				structField, remaining := b.collapseFormalParamInternal(subtyp, fields)
				fields = remaining
				value = b.CreateInsertValue(value, structField, i, "")
			}
			return value, fields
		} else {
			// this struct was not flattened
			return fields[0], fields[1:]
		}
	default:
		return fields[0], fields[1:]
	}
}
