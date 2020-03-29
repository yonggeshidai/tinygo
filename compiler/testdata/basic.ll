target datalayout = "e-m:e-p:32:32-Fi8-i64:64-v128:64:128-a:0:32-n32-S64"
target triple = "armv7m-none-eabi"

define internal i32 @main.add(i32, i32, i8* %context, i8* %parentHandle) unnamed_addr {
entry:
  %2 = add i32 %0, %1
  ret i32 %2
}

define internal void @main.init(i8* %context, i8* %parentHandle) unnamed_addr {
entry:
  ret void
}
