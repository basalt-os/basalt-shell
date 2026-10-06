package voice

import (
	"encoding/binary"
	"math"
)

// Endpointer follows the loudness of a recording while it is made, so
// "press to start and stop" can also end by itself after the person
// stops talking. It is a plain energy detector over 20 ms frames with a
// noise floor that adapts to the room: cheap enough to run on every
// buffer of the capture, with no model. It only decides when to close
// the microphone; whether there was speech, and what was said, is still
// decided by whisper.cpp with Silero VAD on the whole utterance (a stop
// on noise gives "I did not hear anything", never a request).
//
// Time is audio time (frames fed), not the wall clock, so the result
// does not depend on how the capture is scheduled.
type Endpointer struct {
	// Rate is the sample rate of the 16-bit mono PCM fed (16000).
	Rate int

	rem      []byte  // bytes of an incomplete frame
	frames   int64   // frames seen
	floor    float64 // noise floor (RMS, 0..1)
	run      int     // consecutive loud frames
	heard    bool    // a run long enough to be speech was seen
	lastLoud int64   // frame index of the end of the last speech run
}

const (
	epFrameMS = 20
	// A frame is loud when its RMS is over both the absolute minimum
	// (quiet rooms) and epRatio times the noise floor (about 9.5 dB).
	epMinLevel = 0.01
	epRatio    = 3.0
	// Speech is a run of at least epSpeechFrames loud frames (80 ms): a
	// click or a tap on the desk is shorter.
	epSpeechFrames = 4
	// The noise floor is learned from the first frames, then follows the
	// room slowly while nobody speaks.
	epLearnFrames = 10
)

func (e *Endpointer) frameBytes() int {
	r := e.Rate
	if r <= 0 {
		r = 16000
	}
	return r * 2 * epFrameMS / 1000
}

// Feed adds PCM bytes (16-bit little endian, mono) of any length.
func (e *Endpointer) Feed(pcm []byte) {
	fb := e.frameBytes()
	if len(e.rem) > 0 {
		need := fb - len(e.rem)
		if len(pcm) < need {
			e.rem = append(e.rem, pcm...)
			return
		}
		e.rem = append(e.rem, pcm[:need]...)
		e.frame(e.rem)
		e.rem = e.rem[:0]
		pcm = pcm[need:]
	}
	for len(pcm) >= fb {
		e.frame(pcm[:fb])
		pcm = pcm[fb:]
	}
	if len(pcm) > 0 {
		e.rem = append(e.rem[:0], pcm...)
	}
}

func frameRMS(f []byte) float64 {
	n := len(f) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(f[2*i:]))) / 32768
		sum += v * v
	}
	return math.Sqrt(sum / float64(n))
}

func (e *Endpointer) frame(f []byte) {
	lvl := frameRMS(f)
	e.frames++
	if e.frames <= epLearnFrames {
		// Learning the room: the quietest frames set the floor.
		if e.frames == 1 || lvl < e.floor {
			e.floor = lvl
		}
	}
	loud := lvl > epMinLevel && lvl > e.floor*epRatio
	if loud {
		e.run++
		if e.run >= epSpeechFrames {
			e.heard = true
			e.lastLoud = e.frames
		}
		return
	}
	e.run = 0
	// Quiet: the floor follows the room (down fast, up slowly).
	if lvl < e.floor {
		e.floor = 0.7*e.floor + 0.3*lvl
	} else {
		e.floor = 0.995*e.floor + 0.005*lvl
	}
}

// Heard reports whether speech-like sound was seen.
func (e *Endpointer) Heard() bool { return e.heard }

// SilenceMS is the audio time since the last speech (0 before any).
func (e *Endpointer) SilenceMS() int64 {
	if !e.heard {
		return 0
	}
	return (e.frames - e.lastLoud) * epFrameMS
}

// AudioMS is the audio time fed.
func (e *Endpointer) AudioMS() int64 { return e.frames * epFrameMS }
