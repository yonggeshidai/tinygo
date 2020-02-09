
.section .text.runtime.getCurrentStackPointer
.global runtime.getCurrentStackPointer
runtime.getCurrentStackPointer:
    // The stack pointer is located in a1. Copy it over to the return register
    // in a2.
    mov a2, a1
    ret.n

.section .text.runtime.abort
.global runtime.abort
runtime.abort:
1:
    waiti 0 // wait for interrupt
    j 1b
