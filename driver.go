// Package waveshare7in5v2 implements a driver for the waveshare 7in5 V2 e-Paper display
// to be used on a Raspberry Pi board.
//
// A simple driver Epd is implemented that closely follows the official C/Python examples
// provided by waveshare. There is also a Canvas that implements draw.Image allowing to use
// any compatible package to draw to the display.
//
// Datasheet:   https://www.waveshare.com/w/upload/6/60/7.5inch_e-Paper_V2_Specification.pdf
// C code:      https://github.com/waveshare/e-Paper/blob/master/RaspberryPi_JetsonNano/c/lib/e-Paper/EPD_7in5_V2.c
// Python code: https://github.com/waveshare/e-Paper/blob/master/RaspberryPi_JetsonNano/python/lib/waveshare_epd/epd7in5_V2.py
package waveshare7in5v2

import (
	"image"
	"log"

	"github.com/stianeikeland/go-rpio/v4"
)

// The driver to interact with the e-paper display
type Epd struct {
	dc   rpio.Pin
	cs   rpio.Pin
	rst  rpio.Pin
	busy rpio.Pin

	bounds     image.Rectangle
	bufferSize int
	pixelWidth int
}

func New() (*Epd, error) {
	if err := rpio.Open(); err != nil {
		return nil, err
	}

	if err := rpio.SpiBegin(rpio.Spi0); err != nil {
		return nil, err
	}

	rpio.SpiChipSelect(0)

	dc := rpio.Pin(25)
	cs := rpio.Pin(8)
	rst := rpio.Pin(17)
	busy := rpio.Pin(24)

	dc.Output()
	cs.Output()
	rst.Output()
	busy.Input()

	bounds := image.Rect(0, 0, EPD_WIDTH, EPD_HEIGHT)
	pixelWidth := EPD_WIDTH / PIXEL_SIZE
	bufferSize := pixelWidth * EPD_HEIGHT

	d := &Epd{
		dc:   dc,
		cs:   cs,
		rst:  rst,
		busy: busy,

		bounds:     bounds,
		bufferSize: bufferSize,
		pixelWidth: pixelWidth,
	}

	return d, nil
}

// Powers on the screen after power off or sleep.
func (e *Epd) Init() {
	log.Println("Initializing display")
	e.reset()

	e.sendCommandWithData(0x01, // POWER_SETTING
		[]byte{
			0x07,
			0x07, // VGH=20V,VGL=-20V
			0x3f, // VDH=15v
			0x3f, // VDL=-15v
		})

	e.sendCommandWithData(0x06, // BOOSTER_SOFT_START
		[]byte{
			0x17,
			0x17,
			0x28,
			0x17,
		})

	e.sendCommand(0x04) // POWER_ON
	wait(100)
	e.waitUntilIdle() // waiting for the electronic paper IC to release the idle signal

	e.sendCommandWithData(0x00, // PANEL_SETTING
		[]byte{
			0x1F, // KW-3f  KWR-2F	BWROTP 0f	BWOTP 1f
		})

	e.sendCommandWithData(0x61, //tres
		[]byte{
			0x03, // source 800
			0x20,
			0x01, // gate 480
			0xE0,
		})

	e.sendCommandWithData(0x15, []byte{0x00})

	e.sendCommandWithData(0x50, // VCOM AND DATA INTERVAL SETTING
		[]byte{
			0x10,
			0x17,
		})

	e.sendCommandWithData(0x52, []byte{
		0x03,
	})

	e.sendCommandWithData(0x60, []byte{ // TCON SETTING
		0x22,
	})

	log.Println("Display initialized")
}

// Returns the current screen bounds
func (e *Epd) Bounds() image.Rectangle {
	return e.bounds
}

// Converts an image into a buffer array ready to be sent to the display.
// Due to the display only supporting 2 colors a threshold is applied to convert the image to pure black and white.
// The returned buffer is ready to be sent using UpdateFrame.
func (e *Epd) getBuffer(img image.Image, threshold uint8) []byte {
	log.Println("Getting buffer")
	buffer := make([]byte, e.bufferSize)

	for y := 0; y < e.bounds.Dy(); y++ {
		for x := 0; x < e.bounds.Dx(); x += PIXEL_SIZE {
			// Start with white
			var pixel byte = 0x00

			// Iterate and append over the next 8 pixels
			for px := 0; px < PIXEL_SIZE; px++ {
				if isBlack(img.At(x+px, y), threshold) {
					pixel |= (0x80 >> px)
				}
			}

			buffer[(y*e.pixelWidth + x/PIXEL_SIZE)] = pixel
		}
	}

	log.Println("Buffer ready")
	return buffer
}

func (e *Epd) display(buffer []byte) {
	log.Println("Displaying buffer")
	e.sendCommand(0x10)
	e.sendData(buffer)

	e.sendCommand(0x13)
	e.sendData(buffer)

	e.turnOnDisplay()
	log.Println("Buffer displayed")
}

func (e *Epd) turnOnDisplay() {
	log.Println("Turning on display")
	e.sendCommand(0x12)
	wait(100)
	e.waitUntilIdle()
	log.Println("Display turned on")
}

// Allows to easily send an image.Image directly to the screen.
func (e *Epd) DisplayImage(img image.Image) {
	log.Println("Displaying image")
	buffer := e.getBuffer(img, 199)
	e.display(buffer)
	log.Println("Image displayed")
}

// Clear the buffer and updates the screen right away.
func (e *Epd) Clear() {
	log.Println("Clearing display")

	w := 0
	if EPD_WIDTH%PIXEL_SIZE == 0 {
		w = EPD_WIDTH / PIXEL_SIZE
	} else {
		w = EPD_WIDTH/PIXEL_SIZE + 1
	}
	h := EPD_HEIGHT

	img := make([]byte, EPD_WIDTH/8)
	e.sendCommand(0x10)
	for i := 0; i < w; i++ {
		img[i] = 0xFF
	}
	for i := 0; i < h; i++ {
		e.sendData(img)
	}

	e.sendCommand(0x13)
	for i := 0; i < w; i++ {
		img[i] = 0x00
	}
	for i := 0; i < h; i++ {
		e.sendData(img)
	}

	e.turnOnDisplay()

	log.Println("Display cleared")
}

// Puts the display to sleep and powers off. This helps ensure the display longevity
// since keeping it powered on for long periods of time can damage the screen.
// After Sleep the display needs to be woken up by running Init again
func (e *Epd) Sleep() {
	log.Println("Putting display to sleep")
	e.sendCommand(POWER_OFF)
	e.waitUntilIdle()

	e.sendCommandWithData(DEEP_SLEEP, []byte{0xa5})

	wait(2000)

	log.Println("Display is asleep")
}

// Powers off the display and closes the SPI connection.
func (e *Epd) Close() {
	log.Println("Closing display")
	e.cs.Write(rpio.Low)
	e.dc.Write(rpio.Low)
	e.rst.Write(rpio.Low)

	rpio.SpiEnd(rpio.Spi0)
	rpio.Close()
	log.Println("Display closed")
}

func (e *Epd) reset() {
	log.Println("Resetting display")
	e.rst.Write(rpio.High)
	wait(20)
	e.rst.Write(rpio.Low)
	wait(2)
	e.rst.Write(rpio.High)
	wait(20)
	log.Println("Display reset")
}
