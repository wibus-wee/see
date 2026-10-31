//go:build darwin

package app

import (
	"bytes"
	"image"
	"image/color"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/ui"
	_ "golang.org/x/image/tiff"
)

var (
	symbolRuntimeOnce sync.Once
	symbolMessage     uintptr
)

func symbolRuntime() uintptr {
	symbolRuntimeOnce.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			return
		}
		lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		symbolMessage, _ = purego.Dlsym(lib, "objc_msgSend")
	})
	return symbolMessage
}

// AppKit supplies symbols on the user's machine. The cached white alpha mask
// is tinted by MyGo at paint time; no Apple artwork is shipped with See.
func loadSystemSymbol(name string) *symbolImage {
	result := &symbolImage{}
	if name == "" {
		return result
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	msg := symbolRuntime()
	if msg == 0 {
		return result
	}
	call := func(obj uintptr, selector string, args ...uintptr) uintptr {
		a := []uintptr{obj, uintptr(objc.RegisterName(selector))}
		a = append(a, args...)
		ret, _, _ := purego.SyscallN(msg, a...)
		return ret
	}
	pool := call(uintptr(objc.GetClass("NSAutoreleasePool")), "new")
	defer call(pool, "drain")
	nameBytes := append([]byte(name), 0)
	str := call(uintptr(objc.GetClass("NSString")), "stringWithUTF8String:", uintptr(unsafe.Pointer(&nameBytes[0])))
	runtime.KeepAlive(nameBytes)
	imageClass := uintptr(objc.GetClass("NSImage"))
	if call(imageClass, "respondsToSelector:", uintptr(objc.RegisterName("imageWithSystemSymbolName:accessibilityDescription:"))) == 0 {
		return result
	}
	img := call(imageClass, "imageWithSystemSymbolName:accessibilityDescription:", str, 0)
	if img == 0 {
		return result
	}
	var config func(uintptr, objc.SEL, float64, float64) uintptr
	purego.RegisterFunc(&config, msg)
	configClass := uintptr(objc.GetClass("NSImageSymbolConfiguration"))
	if configClass == 0 {
		return result
	}
	cfg := config(configClass, objc.RegisterName("configurationWithPointSize:weight:"), 64, 0)
	if cfg != 0 {
		img = call(img, "imageWithSymbolConfiguration:", cfg)
	}
	if img == 0 {
		return result
	}
	data := call(img, "TIFFRepresentation")
	if data == 0 {
		return result
	}
	n := call(data, "length")
	ptr := call(data, "bytes")
	if n == 0 || ptr == 0 {
		return result
	}
	encoded := append([]byte(nil), unsafe.Slice(*(**byte)(unsafe.Pointer(&ptr)), int(n))...)
	src, _, err := image.Decode(bytes.NewReader(encoded))
	if err != nil {
		return result
	}
	bounds := src.Bounds()
	if bounds.Empty() {
		return result
	}
	mask := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			_, _, _, a := src.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			mask.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: uint8(a >> 8)})
		}
	}
	result.bitmap = ui.NewBitmap(mask)
	result.width, result.height = float32(bounds.Dx()), float32(bounds.Dy())
	return result
}
