// +build esp8266

package machine

import (
	"device/esp"
)

func CPUFrequency() uint32 {
	return 80000000 // 80MHz
}

type PinMode uint8

const (
	PinOutput PinMode = iota
	PinInput
)

// Pins that are fixed by the chip.
const (
	UART_TX_PIN Pin = 1
	UART_RX_PIN Pin = 3
)

// Pin functions are not trivial. The below array maps a pin number (GPIO
// number) to the pad as used in the IO mux.
// Tables with the mapping:
// https://www.esp8266.com/wiki/doku.php?id=esp8266_gpio_pin_allocations#pin_functions
// https://www.espressif.com/sites/default/files/documentation/ESP8266_Pin_List_0.xls
var pinPadMapping = [...]uint8{
	12: 0,
	13: 1,
	14: 2,
	15: 3,
	3:  4,
	1:  5,
	6:  6,
	7:  7,
	8:  8,
	9:  9,
	10: 10,
	11: 11,
	0:  12,
	2:  13,
	4:  14,
	5:  15,
}

// Configure sets the given pin as output or input pin.
func (p Pin) Configure(config PinConfig) {
	switch config.Mode {
	case PinInput, PinOutput:
		pad := pinPadMapping[p]
		if pad >= 12 { // pin 0, 2, 4, 5
			esp.IO_MUX.PIN[pad].Set(0 << 4) // function 0 at bit position 4
		} else {
			esp.IO_MUX.PIN[pad].Set(3 << 4) // function 3 at bit position 4
		}
		if config.Mode == PinOutput {
			esp.GPIO.ENABLE_W1TS.Set(1 << uint8(p))
		} else {
			esp.GPIO.ENABLE_W1TC.Set(1 << uint8(p))
		}
	}
}

// Set sets the output value of this pin to high (true) or low (false).
func (p Pin) Set(value bool) {
	if value {
		esp.GPIO.OUT_W1TS.Set(1 << uint8(p))
	} else {
		esp.GPIO.OUT_W1TC.Set(1 << uint8(p))
	}
}

// UART0 is a hardware UART that supports both TX and RX.
var UART0 = UART{Buffer: NewRingBuffer()}

type UART struct {
	Buffer *RingBuffer
}

// Configure the UART baud rate. TX and RX pins are fixed by the hardware so
// cannot be modified and will be ignored.
func (uart UART) Configure(config UARTConfig) {
	if config.BaudRate == 0 {
		config.BaudRate = 115200
	}
	esp.UART0.CLKDIV.Set(CPUFrequency() / config.BaudRate)
}

// WriteByte writes a single byte to the output buffer. Note that the hardware
// includes a buffer of 128 bytes which will be used first.
func (uart UART) WriteByte(c byte) {
	for (esp.UART0.STATUS.Get()>>16)&0xff >= 128 {
		// Wait until the TX buffer has room.
	}
	esp.UART0.FIFO.Set(uint32(c))
}
