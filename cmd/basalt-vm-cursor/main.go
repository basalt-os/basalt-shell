// Command basalt-vm-cursor hides the pointer that a virtual machine viewer
// draws on its own, before a session that draws the pointer itself starts.
//
// basalt-session runs it, as the person, right before sway, when the
// display adapter is virtual and the session uses a software cursor
// (WLR_NO_HARDWARE_CURSORS=1). QEMU passes the guest's cursor plane to the
// viewer (virt-manager, remote-viewer, VNC), which draws that image at the
// local pointer. With no cursor plane in use QEMU never defines a cursor
// (the viewer keeps its own pointer, so the person sees two) or keeps the
// last one an earlier session left. Setting a fully transparent cursor
// once and turning it off makes QEMU define an empty cursor and hide it:
// only the pointer drawn into the frame by the session remains.
//
// It uses the legacy cursor ioctl on the first DRM card it can drive. It
// needs DRM master, which the first opener of a card gets while no
// compositor runs; otherwise it changes nothing. It never fails the
// session: errors are reported and the exit status is 0.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"unsafe"
)

const cursorSize = 64 // virtio-gpu, QXL and VMware SVGA use 64x64 cursors

// DRM ioctl numbers: _IOWR('d', nr, size) and _IO('d', nr).
func iowr(nr, size uintptr) uintptr { return 3<<30 | size<<16 | 'd'<<8 | nr }

var (
	ioctlSetMaster   = uintptr('d'<<8 | 0x1e)
	ioctlDropMaster  = uintptr('d'<<8 | 0x1f)
	ioctlGetResource = iowr(0xA0, unsafe.Sizeof(cardRes{}))
	ioctlCursor      = iowr(0xA3, unsafe.Sizeof(modeCursor{}))
	ioctlCreateDumb  = iowr(0xB2, unsafe.Sizeof(createDumb{}))
	ioctlMapDumb     = iowr(0xB3, unsafe.Sizeof(mapDumb{}))
	ioctlDestroyDumb = iowr(0xB4, unsafe.Sizeof(destroyDumb{}))
)

// Kernel structs (include/uapi/drm/drm_mode.h).
type cardRes struct {
	FbIDPtr, CrtcIDPtr, ConnectorIDPtr, EncoderIDPtr     uint64
	CountFbs, CountCrtcs, CountConnectors, CountEncoders uint32
	MinWidth, MaxWidth, MinHeight, MaxHeight             uint32
}

type modeCursor struct {
	Flags, CrtcID uint32
	X, Y          int32
	Width, Height uint32
	Handle        uint32
}

type createDumb struct {
	Height, Width, Bpp, Flags uint32
	Handle, Pitch             uint32
	Size                      uint64
}

type mapDumb struct {
	Handle, Pad uint32
	Offset      uint64
}

type destroyDumb struct{ Handle uint32 }

const cursorBO = 0x01 // DRM_MODE_CURSOR_BO

func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

func crtcs(fd int) ([]uint32, error) {
	var r cardRes
	if err := ioctl(fd, ioctlGetResource, unsafe.Pointer(&r)); err != nil {
		return nil, fmt.Errorf("resources: %w", err)
	}
	if r.CountCrtcs == 0 {
		return nil, nil
	}
	ids := make([]uint32, r.CountCrtcs)
	r2 := cardRes{CrtcIDPtr: uint64(uintptr(unsafe.Pointer(&ids[0]))), CountCrtcs: r.CountCrtcs}
	if err := ioctl(fd, ioctlGetResource, unsafe.Pointer(&r2)); err != nil {
		return nil, fmt.Errorf("resources: %w", err)
	}
	return ids[:r2.CountCrtcs], nil
}

// blank sets a transparent cursor on every CRTC of the card, then turns
// the cursor off. It returns how many CRTCs took the cursor.
func blank(card string) (int, error) {
	fd, err := syscall.Open(card, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return 0, err
	}
	defer syscall.Close(fd)
	// The first opener is master already when no compositor runs; this
	// only fails when another program holds the card.
	if err := ioctl(fd, ioctlSetMaster, nil); err != nil {
		return 0, fmt.Errorf("not DRM master (a compositor is running?): %w", err)
	}
	defer ioctl(fd, ioctlDropMaster, nil) //nolint:errcheck // closing drops it too

	ids, err := crtcs(fd)
	if err != nil {
		return 0, err
	}
	cd := createDumb{Width: cursorSize, Height: cursorSize, Bpp: 32}
	if err := ioctl(fd, ioctlCreateDumb, unsafe.Pointer(&cd)); err != nil {
		return 0, fmt.Errorf("cursor buffer: %w", err)
	}
	defer ioctl(fd, ioctlDestroyDumb, unsafe.Pointer(&destroyDumb{Handle: cd.Handle})) //nolint:errcheck
	// Fully transparent (ARGB 0). New buffers are usually zeroed already;
	// buffers in video memory are not.
	md := mapDumb{Handle: cd.Handle}
	if err := ioctl(fd, ioctlMapDumb, unsafe.Pointer(&md)); err != nil {
		return 0, fmt.Errorf("map cursor buffer: %w", err)
	}
	mem, err := syscall.Mmap(fd, int64(md.Offset), int(cd.Size), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return 0, fmt.Errorf("map cursor buffer: %w", err)
	}
	clear(mem)
	syscall.Munmap(mem) //nolint:errcheck

	n := 0
	var last error
	for _, id := range ids {
		on := modeCursor{Flags: cursorBO, CrtcID: id, Width: cursorSize, Height: cursorSize, Handle: cd.Handle}
		if err := ioctl(fd, ioctlCursor, unsafe.Pointer(&on)); err != nil {
			last = err // an inactive CRTC, or a card without a cursor plane
			continue
		}
		// Handle 0 turns it off: QEMU keeps the empty image and hides it.
		off := modeCursor{Flags: cursorBO, CrtcID: id}
		if err := ioctl(fd, ioctlCursor, unsafe.Pointer(&off)); err != nil {
			last = err
			continue
		}
		n++
	}
	if n == 0 && last != nil {
		return 0, fmt.Errorf("cursor: %w", last)
	}
	return n, nil
}

func main() {
	cards, _ := filepath.Glob("/dev/dri/card[0-9]*")
	sort.Strings(cards)
	if len(cards) == 0 {
		fmt.Fprintln(os.Stderr, "basalt-vm-cursor: no DRM card")
		return
	}
	for _, card := range cards {
		n, err := blank(card)
		if err != nil {
			fmt.Fprintf(os.Stderr, "basalt-vm-cursor: %s: %v\n", card, err)
			continue
		}
		if n > 0 {
			fmt.Printf("basalt-vm-cursor: %s: viewer cursor hidden on %d display(s)\n", card, n)
		}
	}
}
