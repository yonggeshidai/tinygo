// +build esp8266

// Most of the register information in here comes from the appendix in this
// document:
// https://www.espressif.com/sites/default/files/documentation/esp8266-technical_reference_en.pdf
// Some more information comes from the *_register.h files here:
// https://github.com/esp8266/Arduino/tree/master/tools/sdk/include

package esp

import (
	"runtime/volatile"
	"unsafe"
)

var (
	UART0  = (*UART_Type)(unsafe.Pointer(uintptr(0x60000000)))
	GPIO   = (*GPIO_Type)(unsafe.Pointer(uintptr(0x60000300)))
	FRC1   = (*FRC_Type)(unsafe.Pointer(uintptr(0x60000600))) // timer 1
	FRC2   = (*FRC_Type)(unsafe.Pointer(uintptr(0x60000620))) // timer 2
	IO_MUX = (*IO_MUX_Type)(unsafe.Pointer(uintptr(0x60000800)))
)

type UART_Type struct {
	FIFO      volatile.Register32
	INT_RAW   volatile.Register32
	INT_ST    volatile.Register32
	INT_ENA   volatile.Register32
	INT_CLR   volatile.Register32
	CLKDIV    volatile.Register32
	AUTOBAUD  volatile.Register32
	STATUS    volatile.Register32
	CONF0     volatile.Register32
	CONF1     volatile.Register32
	LOWPULSE  volatile.Register32
	HIGHPULSE volatile.Register32
	RXD_CNT   volatile.Register32
	DATE      volatile.Register32
	ID        volatile.Register32
}

type GPIO_Type struct {
	OUT             volatile.Register32
	OUT_W1TS        volatile.Register32
	OUT_W1TC        volatile.Register32
	ENABLE          volatile.Register32
	ENABLE_W1TS     volatile.Register32
	ENABLE_W1TC     volatile.Register32
	IN              volatile.Register32
	STATUS          volatile.Register32
	STATUS_W1TS     volatile.Register32
	STATUS_W1TC     volatile.Register32
	PIN             [16]volatile.Register32
	SIGMA_DELTA     volatile.Register32
	RTC_CALIB_SYNC  volatile.Register32
	RTC_CALIB_VALUE volatile.Register32
}

type FRC_Type struct {
	LOAD  volatile.Register32
	COUNT volatile.Register32
	CTRL  volatile.Register32
	INT   volatile.Register32
	ALARM volatile.Register32 // only available in FRC2
}

type IO_MUX_Type struct {
	CONF volatile.Register32
	// The pins appear to have the following bits defined:
	//   bit 0:   output enable
	//   bit 1:   sleep output enable
	//   bit 2:   sleep pullup 2
	//   bit 3:   sleep pullup
	//   bit 4-5: lower bits of the pin function
	//   bit 6:   pullup 2
	//   bit 7:   pullup
	//   bit 8:   upper bit of the pin function
	// Source:
	// https://github.com/esp8266/Arduino/blob/11ae243ecf00bd80c1d5aacde95ca20e92e2cb74/tools/sdk/include/eagle_soc.h#L217-L224
	PIN [17]volatile.Register32
}
