package compiler

// This file implements function values and closures. It may need some lowering
// in a later step, see func-lowering.go.

import (
	"go/types"
	"strings"

	"github.com/tinygo-org/tinygo/compileopts"
	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// createFuncValue creates a function value from a raw function pointer with no
// context.
func (b *builder) createFuncValue(funcPtr, context llvm.Value, sig *types.Signature) llvm.Value {
	return b.compilerContext.createFuncValue(b.Builder, funcPtr, context, sig)
}

// createFuncValue creates a function value from a raw function pointer with no
// context.
func (c *compilerContext) createFuncValue(builder llvm.Builder, funcPtr, context llvm.Value, sig *types.Signature) llvm.Value {
	var funcValueScalar llvm.Value
	switch c.FuncImplementation() {
	case compileopts.FuncValueDoubleword:
		// Closure is: {context, function pointer}
		funcValueScalar = funcPtr
	case compileopts.FuncValueSwitch:
		sigGlobal := c.getTypeCode(sig)
		funcValueWithSignatureGlobalName := funcPtr.Name() + "$withSignature"
		funcValueWithSignatureGlobal := c.mod.NamedGlobal(funcValueWithSignatureGlobalName)
		if funcValueWithSignatureGlobal.IsNil() {
			funcValueWithSignatureType := c.getLLVMRuntimeType("funcValueWithSignature")
			funcValueWithSignature := llvm.ConstNamedStruct(funcValueWithSignatureType, []llvm.Value{
				llvm.ConstPtrToInt(funcPtr, c.uintptrType),
				sigGlobal,
			})
			funcValueWithSignatureGlobal = llvm.AddGlobal(c.mod, funcValueWithSignatureType, funcValueWithSignatureGlobalName)
			funcValueWithSignatureGlobal.SetInitializer(funcValueWithSignature)
			funcValueWithSignatureGlobal.SetGlobalConstant(true)
			funcValueWithSignatureGlobal.SetLinkage(llvm.InternalLinkage)
		}
		funcValueScalar = llvm.ConstPtrToInt(funcValueWithSignatureGlobal, c.uintptrType)
	default:
		panic("unimplemented func value variant")
	}
	funcValueType := c.getFuncType(sig)
	funcValue := llvm.Undef(funcValueType)
	funcValue = builder.CreateInsertValue(funcValue, context, 0, "")
	funcValue = builder.CreateInsertValue(funcValue, funcValueScalar, 1, "")
	return funcValue
}

// extractFuncScalar returns some scalar that can be used in comparisons. It is
// a cheap operation.
func (b *builder) extractFuncScalar(funcValue llvm.Value) llvm.Value {
	return b.CreateExtractValue(funcValue, 1, "")
}

// extractFuncContext extracts the context pointer from this function value. It
// is a cheap operation.
func (b *builder) extractFuncContext(funcValue llvm.Value) llvm.Value {
	return b.CreateExtractValue(funcValue, 0, "")
}

// decodeFuncValue extracts the context and the function pointer from this func
// value. This may be an expensive operation.
func (b *builder) decodeFuncValue(funcValue llvm.Value, sig *types.Signature) (funcPtr, context llvm.Value) {
	context = b.CreateExtractValue(funcValue, 0, "")
	switch b.FuncImplementation() {
	case compileopts.FuncValueDoubleword:
		funcPtr = b.CreateExtractValue(funcValue, 1, "")
	case compileopts.FuncValueSwitch:
		llvmSig := b.getRawFuncType(sig)
		sigGlobal := b.getTypeCode(sig)
		funcPtr = b.createRuntimeCall("getFuncPtr", []llvm.Value{funcValue, sigGlobal}, "")
		funcPtr = b.CreateIntToPtr(funcPtr, llvmSig, "")
	default:
		panic("unimplemented func value variant")
	}
	return
}

// getFuncType returns the type of a func value given a signature.
func (c *compilerContext) getFuncType(typ *types.Signature) llvm.Type {
	switch c.FuncImplementation() {
	case compileopts.FuncValueDoubleword:
		rawPtr := c.getRawFuncType(typ)
		return c.ctx.StructType([]llvm.Type{c.i8ptrType, rawPtr}, false)
	case compileopts.FuncValueSwitch:
		return c.getLLVMRuntimeType("funcValue")
	default:
		panic("unimplemented func value variant")
	}
}

// getRawFuncType returns a LLVM function pointer type for a given signature.
func (c *compilerContext) getRawFuncType(typ *types.Signature) llvm.Type {
	// Get the return type.
	var returnType llvm.Type
	switch typ.Results().Len() {
	case 0:
		// No return values.
		returnType = c.ctx.VoidType()
	case 1:
		// Just one return value.
		returnType = c.getLLVMType(typ.Results().At(0).Type())
	default:
		// Multiple return values. Put them together in a struct.
		// This appears to be the common way to handle multiple return values in
		// LLVM.
		members := make([]llvm.Type, typ.Results().Len())
		for i := 0; i < typ.Results().Len(); i++ {
			members[i] = c.getLLVMType(typ.Results().At(i).Type())
		}
		returnType = c.ctx.StructType(members, false)
	}

	// Get the parameter types.
	var paramTypes []llvm.Type
	if typ.Recv() != nil {
		recv := c.getLLVMType(typ.Recv().Type())
		if recv.StructName() == "runtime._interface" {
			// This is a call on an interface, not a concrete type.
			// The receiver is not an interface, but a i8* type.
			recv = c.i8ptrType
		}
		recvFragments, _ := expandFormalParamType(recv, nil)
		paramTypes = append(paramTypes, recvFragments...)
	}
	for i := 0; i < typ.Params().Len(); i++ {
		subType := c.getLLVMType(typ.Params().At(i).Type())
		paramTypeFragments, _ := expandFormalParamType(subType, nil)
		paramTypes = append(paramTypes, paramTypeFragments...)
	}
	// All functions take these parameters at the end.
	paramTypes = append(paramTypes, c.i8ptrType) // context
	paramTypes = append(paramTypes, c.i8ptrType) // parent coroutine

	// Make a func type out of the signature.
	return llvm.PointerType(llvm.FunctionType(returnType, paramTypes, false), c.funcPtrAddrSpace)
}

// parseMakeClosure makes a function value (with context) from the given
// closure expression.
func (b *builder) parseMakeClosure(expr *ssa.MakeClosure) (llvm.Value, error) {
	if len(expr.Bindings) == 0 {
		panic("unexpected: MakeClosure without bound variables")
	}
	f := expr.Fn.(*ssa.Function)
	llvmFn := b.getFunction(f)

	if strings.HasSuffix(f.Name(), "$bound") && llvmFn.IsDeclaration() {
		// Hack: the ssa package does not expose bound methods so make sure
		// they're built here when necessary.
		irbuilder := b.ctx.NewBuilder()
		defer irbuilder.Dispose()
		b.createFunction(irbuilder, f, llvmFn)
	}

	// Collect all bound variables.
	boundVars := make([]llvm.Value, len(expr.Bindings))
	for i, binding := range expr.Bindings {
		// The context stores the bound variables.
		llvmBoundVar := b.getValue(binding)
		boundVars[i] = llvmBoundVar
	}

	// Store the bound variables in a single object, allocating it on the heap
	// if necessary.
	context := b.emitPointerPack(boundVars)

	// Create the closure.
	return b.createFuncValue(llvmFn, context, f.Signature), nil
}
