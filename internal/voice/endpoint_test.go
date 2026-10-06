package voice

import (
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
)

// pcm makes ms milliseconds of 16 kHz audio: a 220 Hz tone of amplitude
// amp (0..1) over white noise of amplitude noise.
func pcm(ms int, amp, noise float64, rng *rand.Rand) []byte {
	n := 16000 * ms / 1000
	b := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		v := amp*math.Sin(2*math.Pi*220*float64(i)/16000) + noise*(rng.Float64()*2-1)
		binary.LittleEndian.PutUint16(b[2*i:], uint16(int16(v*32767)))
	}
	return b
}

func TestEndpointerSpeechThenSilence(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	e := &Endpointer{Rate: 16000}
	e.Feed(pcm(500, 0, 0.003, rng)) // the room
	if e.Heard() || e.SilenceMS() != 0 {
		t.Fatalf("room noise counted as speech")
	}
	e.Feed(pcm(1200, 0.2, 0.003, rng)) // speech
	if !e.Heard() {
		t.Fatalf("speech not heard")
	}
	if e.SilenceMS() > 20 {
		t.Fatalf("silence during speech: %d", e.SilenceMS())
	}
	e.Feed(pcm(2000, 0, 0.003, rng)) // quiet again
	if s := e.SilenceMS(); s < 1900 || s > 2020 {
		t.Fatalf("silence %d ms, want about 2000", s)
	}
}

func TestEndpointerIgnoresClicksAndOddChunks(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	e := &Endpointer{Rate: 16000}
	e.Feed(pcm(400, 0, 0.002, rng))
	// A 40 ms tap is not speech.
	e.Feed(pcm(40, 0.5, 0.002, rng))
	e.Feed(pcm(400, 0, 0.002, rng))
	if e.Heard() {
		t.Fatal("a click was taken for speech")
	}
	// Speech fed in chunks that do not align with frames.
	sp := pcm(600, 0.15, 0.002, rng)
	for len(sp) > 0 {
		n := 333
		if n > len(sp) {
			n = len(sp)
		}
		e.Feed(sp[:n])
		sp = sp[n:]
	}
	if !e.Heard() {
		t.Fatal("speech in odd chunks not heard")
	}
	if got := e.AudioMS(); got < 1420 || got > 1440 {
		t.Fatalf("audio time %d ms", got)
	}
}

func TestEndpointerLoudRoom(t *testing.T) {
	// A fan: steady noise well over the absolute minimum is the floor,
	// not speech; speech over it is still heard.
	rng := rand.New(rand.NewSource(3))
	e := &Endpointer{Rate: 16000}
	e.Feed(pcm(3000, 0, 0.04, rng))
	if e.Heard() {
		t.Fatal("steady noise taken for speech")
	}
	e.Feed(pcm(500, 0.3, 0.04, rng))
	if !e.Heard() {
		t.Fatal("speech over noise not heard")
	}
}
