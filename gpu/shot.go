package gpu

import (
	"context"
	"image"
	"image/png"
	"os"
	"time"

	"github.com/chmenegatti/oiapoque-chui/game"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// screenshot desenha o quadro atual numa textura fora da tela e grava em PNG.
func (r *renderer3d) screenshot(path string, w, h int, g *game.Game) error {
	tex, err := r.dev.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "captura",
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        r.format,
		Usage:         gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageCopySrc,
	})
	if err != nil {
		return err
	}
	defer tex.Release()
	view, err := r.dev.CreateTextureView(tex, nil)
	if err != nil {
		return err
	}
	defer view.Release()
	if err := r.frame(view, w, h, g); err != nil {
		return err
	}

	row := (uint32(w)*4 + 255) &^ 255
	size := uint64(row) * uint64(h)
	buf, err := r.dev.CreateBuffer(&wgpu.BufferDescriptor{Label: "captura", Size: size,
		Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
	if err != nil {
		return err
	}
	defer buf.Release()
	enc, err := r.dev.CreateCommandEncoder(nil)
	if err != nil {
		return err
	}
	enc.CopyTextureToBuffer(tex, buf, []wgpu.BufferTextureCopy{{
		BufferLayout: wgpu.ImageDataLayout{BytesPerRow: row, RowsPerImage: uint32(h)},
		TextureBase:  wgpu.ImageCopyTexture{Texture: tex},
		Size:         wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
	}})
	cmds, err := enc.Finish()
	if err != nil {
		return err
	}
	if _, err := r.dev.Queue().Submit(cmds); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := buf.Map(ctx, wgpu.MapModeRead, 0, size); err != nil {
		return err
	}
	defer buf.Unmap()
	rng, err := buf.MappedRange(0, size)
	if err != nil {
		return err
	}
	data := rng.Bytes()

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	bgra := r.format == gputypes.TextureFormatBGRA8Unorm || r.format == gputypes.TextureFormatBGRA8UnormSrgb
	for y := range h {
		src := data[int(row)*y : int(row)*y+w*4]
		dst := img.Pix[img.Stride*y : img.Stride*y+w*4]
		copy(dst, src)
		for x := 0; x < w*4; x += 4 {
			if bgra {
				dst[x], dst[x+2] = dst[x+2], dst[x]
			}
			dst[x+3] = 255
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
