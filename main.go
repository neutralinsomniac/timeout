package main

/*
typedef unsigned char Uint8;
void OnAudio(void *userdata, Uint8 *stream, int length);
*/
import "C"
import (
	"bufio"
	"fmt"
	movingaverage "github.com/RobinUS2/golang-moving-average"
	"log"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/veandco/go-sdl2/sdl"
	"github.com/veandco/go-sdl2/ttf"
)

const (
	DefaultFrequency = 16000
	DefaultFormat    = sdl.AUDIO_S16
	DefaultChannels  = 1
	DefaultSamples   = 512
)

const (
	fontPath = "font.ttf"
	fontSize = 128
)

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)

	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	return fmt.Sprintf("%02d:%02d", m, s)
}

var (
	audioC = make(chan []int16, 1)
)

//export OnAudio
func OnAudio(userdata unsafe.Pointer, _stream *C.Uint8, _length C.int) {
	// We need to cast the stream from C uint8 array into Go int16 slice
	var buf []int16
	header := (*reflect.SliceHeader)(unsafe.Pointer(&buf))
	length := int(_length) / 2 // Divide by 2 because a single int16 consists of two uint8
	header.Len = length        // Build the slice header for our int16 slice
	header.Cap = length
	header.Data = uintptr(unsafe.Pointer(_stream))

	// Copy the audio samples into temporary buffer
	audioSamples := make([]int16, length)
	copy(audioSamples, buf)

	// Send the temporary buffer to our main function via our Go channel
	audioC <- audioSamples
}

func run(dur time.Duration) (err error) {
	var window *sdl.Window
	var font *ttf.Font
	var surface *sdl.Surface
	var text *sdl.Surface

	var dev sdl.AudioDeviceID

	if err = ttf.Init(); err != nil {
		return
	}
	defer ttf.Quit()

	if err = sdl.Init(sdl.INIT_VIDEO | sdl.INIT_AUDIO); err != nil {
		return
	}
	defer sdl.Quit()

	// Specify the configuration for our default recording device
	spec := sdl.AudioSpec{
		Freq:     DefaultFrequency,
		Format:   DefaultFormat,
		Channels: DefaultChannels,
		Samples:  DefaultSamples,
		Callback: sdl.AudioCallback(C.OnAudio),
	}

	// Open default recording device
	audioDeviceIndex := -1
	numAudioDevices := sdl.GetNumAudioDevices(true)
	switch numAudioDevices {
	case 0:
		log.Fatal("ERROR: no audio devices can capture audio")
	case 1:
		audioDeviceIndex = 0
	default:
		fmt.Println("multiple recording devices found. pick one:")
		for i := 0; i < numAudioDevices; i++ {
			fmt.Printf("%d: %s\n", i, sdl.GetAudioDeviceName(i, true))
		}

		reader := bufio.NewReader(os.Stdin)
		text, _ := reader.ReadString('\n')
		text = strings.TrimSuffix(text, "\n")
		tmpIndex, err := strconv.ParseInt(text, 10, 32)
		if err != nil {
			log.Fatal(err)
		}
		if int(tmpIndex) > numAudioDevices-1 {
			log.Fatal("index out of range")
		}
		audioDeviceIndex = int(tmpIndex)
	}
	defaultRecordingDeviceName := sdl.GetAudioDeviceName(audioDeviceIndex, true)
	if dev, err = sdl.OpenAudioDevice(defaultRecordingDeviceName, true, &spec, nil, 0); err != nil {
		log.Fatal(err)
	}
	defer sdl.CloseAudioDevice(dev)

	// Start recording audio
	sdl.PauseAudioDevice(dev, false)

	// Create a window for us to draw the text on
	if window, err = sdl.CreateWindow("TimeOut", sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED, 800, 600, sdl.WINDOW_SHOWN); err != nil {
		return
	}
	defer window.Destroy()

	if surface, err = window.GetSurface(); err != nil {
		log.Fatal(err)
	}

	// Load the font for our text
	if font, err = ttf.OpenFont(fontPath, fontSize); err != nil {
		log.Fatal(err)
	}
	defer font.Close()

	start := time.Now()

	end := start.Add(dur)

	// Run infinite loop until user closes the window
	movingAvg := movingaverage.New(4096)
	running := true
	remaining := end.Sub(start)
	for running && remaining.Seconds() > 0 {
		for event := sdl.PollEvent(); event != nil; event = sdl.PollEvent() {
			switch event.(type) {
			case *sdl.QuitEvent:
				running = false
			}
		}
		select {
		case audioSamples := <-audioC:
			for i := range audioSamples {
				movingAvg.Add(math.Abs(float64(audioSamples[i])))
			}

			// tweak this to match your soundcard
			if movingAvg.Avg() > 1000 {
				end = start.Add(dur)
			}
		}

		// fmt.Printf("%f\n", movingAvg.Avg())
		// Create a red text with the font
		if text, err = font.RenderUTF8Blended(fmtDuration(remaining), sdl.Color{R: 255, G: 0, B: 0, A: 255}); err != nil {
			return
		}

		// Draw the text around the center of the window
		surface.FillRect(nil, 0)
		if err = text.Blit(nil, surface, &sdl.Rect{X: 400 - (text.W / 2), Y: 300 - (text.H / 2), W: 0, H: 0}); err != nil {
			return
		}

		// Update the window surface with what we have drawn
		window.UpdateSurface()
		//sdl.Delay(16)
		text.Free()
		start = time.Now()
		remaining = end.Sub(start)
	}

	return
}

func main() {
	if len(os.Args) != 2 {
		fmt.Printf("usage: %s <duration>\n", os.Args[0])
		fmt.Printf("valid time units are \"ns\", \"us\" (or \"µs\"), \"ms\", \"s\", \"m\", \"h\"\n")
		fmt.Printf("example: %s 3m20s\n", os.Args[0])
		os.Exit(1)
	}

	dur, err := time.ParseDuration(os.Args[1])
	if err != nil {
		log.Fatal("invalid duration\n")
	}

	if err := run(dur); err != nil {
		os.Exit(1)
	}
}