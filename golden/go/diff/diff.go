package diff

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"os"
	"unsafe"

	"go.skia.org/infra/go/sklog"
	"go.skia.org/infra/go/util"
)

var (
	PixelMatchColor = color.Transparent

	// Orange gradient.
	//
	// These are non-premultiplied RGBA values.
	PixelDiffColor = [][]uint8{
		{0xfd, 0xd0, 0xa2, 0xff},
		{0xfd, 0xae, 0x6b, 0xff},
		{0xfd, 0x8d, 0x3c, 0xff},
		{0xf1, 0x69, 0x13, 0xff},
		{0xd9, 0x48, 0x01, 0xff},
		{0xa6, 0x36, 0x03, 0xff},
		{0x7f, 0x27, 0x04, 0xff},
	}

	// Blue gradient.
	//
	// These are non-premultiplied RGBA values.
	PixelAlphaDiffColor = [][]uint8{
		{0xc6, 0xdb, 0xef, 0xff},
		{0x9e, 0xca, 0xe1, 0xff},
		{0x6b, 0xae, 0xd6, 0xff},
		{0x42, 0x92, 0xc6, 0xff},
		{0x21, 0x71, 0xb5, 0xff},
		{0x08, 0x51, 0x9c, 0xff},
		{0x08, 0x30, 0x6b, 0xff},
	}
)

// Returns the offset into the color slices (PixelDiffColor,
// or PixelAlphaDiffColor) based on the delta passed in.
//
// The number passed in is the difference between two colors,
// on a scale from 1 to 1024.
func deltaOffset(n int) int {
	ret := int(math.Ceil(math.Log(float64(n))/math.Log(3) + 0.5))
	if ret < 1 || ret > 7 {
		sklog.Fatalf("Input: %d", n)
	}
	return ret - 1
}

// DiffMetrics contains the diff information between two images.
type DiffMetrics struct {
	// NumDiffPixels is the absolute number of pixels that are different.
	NumDiffPixels int `json:"numDiffPixels"`

	// PixelDiffPercent is the percentage of pixels that are different.
	PixelDiffPercent float32 `json:"pixelDiffPercent"`

	// MaxRGBADiffs contains the maximum difference of each channel.
	MaxRGBADiffs []int `json:"maxRGBADiffs"`

	// DimDiffer is true if the dimensions between the two images are different.
	DimDiffer bool `json:"dimDiffer"`

	// Diffs contains different diff metrics for the to images.
	Diffs map[string]float32 `json:"diffs"`
}

// Diff error to indicate different error conditions during diffing.
type DiffErr string

const (
	// Http related error occurred.
	HTTP DiffErr = "http_error"

	// Image is corrupted and cannot be decoded.
	CORRUPTED DiffErr = "corrupted"

	// Arbitrary error.
	OTHER DiffErr = "other"
)

// DigestFailure captures the details of a digest error that occurred.
type DigestFailure struct {
	Digest string  `json:"digest"`
	Reason DiffErr `json:"reason"`
	TS     int64   `json:"ts"`
}

// NewDigestFailure is a convenience function to create an instance of DigestFailure.
// It sets the provided arguments in the correct fields and adds a timestamp with
// the current time in milliseconds.
func NewDigestFailure(digest string, reason DiffErr) *DigestFailure {
	return &DigestFailure{
		Digest: digest,
		Reason: reason,
		TS:     util.TimeStampMs(),
	}
}

// Implement sort.Interface for a slice of DigestFailure
type DigestFailureSlice []*DigestFailure

func (d DigestFailureSlice) Len() int           { return len(d) }
func (d DigestFailureSlice) Less(i, j int) bool { return d[i].TS < d[j].TS }
func (d DigestFailureSlice) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }

const (
	// PRIORITY_NOW is the highest priority intended for in request calls.
	PRIORITY_NOW = iota

	// PRIORITY_BACKGROUND is the priority to use for background tasks.
	// i.e. Use to calculate diffs of ignored digests.
	PRIORITY_BACKGROUND

	// PRIORITY_IDLE is the priority to use for background tasks that have
	// very low priority.
	PRIORITY_IDLE
)

// Signature for diff function that takes in two images and returns an
// application dependent diff result structure as a generic interface along with
// the diff image
type DiffFn func(*image.NRGBA, *image.NRGBA) (interface{}, *image.NRGBA)

// DiffStore defines an interface for a type that retrieves, stores and
// diffs images. How it retrieves the images is up to the implementation.
type DiffStore interface {
	// Get returns the DiffMetrics of the provided dMain digest vs all digests
	// specified in dRest.
	Get(priority int64, mainDigest string, rightDigests []string) (map[string]interface{}, error)

	// ImageHandler returns a http.Handler for the given path prefix. The caller
	// can then serve images of the format:
	//        <urlPrefix>/images/<digests>.png
	//        <irlPrefix>/diffs/<digest1>-<digests2>.png
	ImageHandler(urlPrefix string) (http.Handler, error)

	// WarmDigest will fetch the given digests. If sync is true the call will
	// block until all digests have been fetched or failed to fetch.
	WarmDigests(priority int64, digests []string, sync bool)

	// WarmDiffs will calculate the difference between every digests in
	// leftDigests and every in digests in rightDigests.
	WarmDiffs(priority int64, leftDigests []string, rightDigests []string)

	// UnavailableDigests returns map[digest]*DigestFailure which can be used
	// to check whether a digest could not be processed and to provide details
	// about failures.
	UnavailableDigests() map[string]*DigestFailure

	// PurgeDigests removes all information related to the indicated digests
	// (image, diffmetric) from local caches. If purgeGCS is true it will also
	// purge the digests image from Google storage, forcing that the digest
	// be re-uploaded by the build bots.
	PurgeDigests(digests []string, purgeGCS bool) error
}

// OpenImage is a utility function that opens the specified file and returns an
// image.Image
func OpenImage(filePath string) (image.Image, error) {
	reader, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer util.Close(reader)
	im, err := png.Decode(reader)
	if err != nil {
		return nil, err
	}
	return im, nil
}

// Returns the percentage of pixels that differ, as a float between 0 and 100
// (inclusive).
func getPixelDiffPercent(numDiffPixels, totalPixels int) float32 {
	return (float32(numDiffPixels) * 100) / float32(totalPixels)
}

func uint8ToColor(c []uint8) color.Color {
	return color.NRGBA{R: c[0], G: c[1], B: c[2], A: c[3]}
}

// diffColors compares two color values and returns a color to indicate the
// difference. If the colors differ it updates maxRGBADiffs to contain the
// maximum difference over multiple calls.
// If the RGB channels are identical, but the alpha differ then
// PixelAlphaDiffColor is returned. This allows to distinguish pixels that
// render the same, but have different alpha values.
func diffColors(color1, color2 color.Color, maxRGBADiffs []int) color.Color {
	// We compare them before normalizing to non-premultiplied. If one of the
	// original images did not have an alpha channel (but the other did) the
	// equality will be false.
	if color1 == color2 {
		return PixelMatchColor
	}

	// Treat all colors as non-premultiplied.
	c1 := color.NRGBAModel.Convert(color1).(color.NRGBA)
	c2 := color.NRGBAModel.Convert(color2).(color.NRGBA)

	rDiff := util.AbsInt(int(c1.R) - int(c2.R))
	gDiff := util.AbsInt(int(c1.G) - int(c2.G))
	bDiff := util.AbsInt(int(c1.B) - int(c2.B))
	aDiff := util.AbsInt(int(c1.A) - int(c2.A))
	maxRGBADiffs[0] = util.MaxInt(maxRGBADiffs[0], rDiff)
	maxRGBADiffs[1] = util.MaxInt(maxRGBADiffs[1], gDiff)
	maxRGBADiffs[2] = util.MaxInt(maxRGBADiffs[2], bDiff)
	maxRGBADiffs[3] = util.MaxInt(maxRGBADiffs[3], aDiff)

	// If the color channels differ we mark with the diff color.
	if (c1.R != c2.R) || (c1.G != c2.G) || (c1.B != c2.B) {
		// We use the Manhattan metric for color difference.
		return uint8ToColor(PixelDiffColor[deltaOffset(rDiff+gDiff+bDiff+aDiff)])
	}

	// If only the alpha channel differs we mark it with the alpha diff color.
	//
	if aDiff > 0 {
		return uint8ToColor(PixelAlphaDiffColor[deltaOffset(aDiff)])
	}

	return PixelMatchColor
}

// recode creates a new NRGBA image from the given image.
func recode(img image.Image) *image.NRGBA {
	ret := image.NewNRGBA(img.Bounds())
	draw.Draw(ret, img.Bounds(), img, image.Pt(0, 0), draw.Src)
	return ret
}

// GetNRGBA converts the image to an *image.NRGBA in an efficient manner.
func GetNRGBA(img image.Image) *image.NRGBA {
	switch t := img.(type) {
	case *image.NRGBA:
		return t
	case *image.RGBA:
		for i := 0; i < len(t.Pix); i += 4 {
			if t.Pix[i+3] != 0xff {
				sklog.Warning("Unexpected premultiplied image!")
				return recode(img)
			}
		}
		// If every alpha is 0xff then t.Pix is already in NRGBA format, simply
		// share Pix between the RGBA and NRGBA structs.
		return &image.NRGBA{
			Pix:    t.Pix,
			Stride: t.Stride,
			Rect:   t.Rect,
		}
	default:
		// TODO(mtklein): does it make sense we're getting other types, or a DM bug?
		return recode(img)
	}
}

// PixelDiff is a utility function that calculates the DiffMetrics and the image of the
// difference for the provided images.
func PixelDiff(img1, img2 image.Image) (*DiffMetrics, *image.NRGBA) {

	img1Bounds := img1.Bounds()
	img2Bounds := img2.Bounds()

	// Get the bounds we want to compare.
	cmpWidth := util.MinInt(img1Bounds.Dx(), img2Bounds.Dx())
	cmpHeight := util.MinInt(img1Bounds.Dy(), img2Bounds.Dy())

	// Get the bounds of the resulting image. If they dimensions match they
	// will be identical to the result bounds. Fill the image with black pixels.
	resultWidth := util.MaxInt(img1Bounds.Dx(), img2Bounds.Dx())
	resultHeight := util.MaxInt(img1Bounds.Dy(), img2Bounds.Dy())
	resultImg := image.NewNRGBA(image.Rect(0, 0, resultWidth, resultHeight))
	totalPixels := resultWidth * resultHeight

	// Loop through all points and compare. We start assuming all pixels are
	// wrong. This takes care of the case where the images have different sizes
	// and there is an area not inspected by the loop.
	numDiffPixels := totalPixels
	maxRGBADiffs := make([]int, 4)

	// Pix is a []uint8 rotating through R, G, B, A, R, G, B, A, ...
	p1 := GetNRGBA(img1).Pix
	p2 := GetNRGBA(img2).Pix
	// Compare the bounds, if they are the same then use this fast path.
	// We pun to uint64 to compare 2 pixels at a time, so we also require
	// an even number of pixels here.  If that's a big deal, we can easily
	// fix that up, handling the straggler pixel separately at the end.
	if img1Bounds.Eq(img2Bounds) && len(p1)%8 == 0 {
		numDiffPixels = 0
		// Note the += 8.  We're checking two pixels at a time here.
		for i := 0; i < len(p1); i += 8 {
			// Most pixels we compare will be the same, so from here to
			// the 'continue' is the hot path in all this code.
			rgba_2x := (*uint64)(unsafe.Pointer(&p1[i]))
			RGBA_2x := (*uint64)(unsafe.Pointer(&p2[i]))
			if *rgba_2x == *RGBA_2x {
				continue
			}

			// When off == 0, we check the first pixel of the pair; when 4, the second.
			for off := 0; off <= 4; off += 4 {
				r, g, b, a := p1[off+i+0], p1[off+i+1], p1[off+i+2], p1[off+i+3]
				R, G, B, A := p2[off+i+0], p2[off+i+1], p2[off+i+2], p2[off+i+3]
				if r != R || g != G || b != B || a != A {
					numDiffPixels++
					dr := util.AbsInt(int(r) - int(R))
					dg := util.AbsInt(int(g) - int(G))
					db := util.AbsInt(int(b) - int(B))
					da := util.AbsInt(int(a) - int(A))
					maxRGBADiffs[0] = util.MaxInt(dr, maxRGBADiffs[0])
					maxRGBADiffs[1] = util.MaxInt(dg, maxRGBADiffs[1])
					maxRGBADiffs[2] = util.MaxInt(db, maxRGBADiffs[2])
					maxRGBADiffs[3] = util.MaxInt(da, maxRGBADiffs[3])
					if dr+dg+db > 0 {
						copy(resultImg.Pix[off+i:], PixelDiffColor[deltaOffset(dr+dg+db+da)])
					} else {
						copy(resultImg.Pix[off+i:], PixelAlphaDiffColor[deltaOffset(da)])
					}
				}
			}
		}
	} else {
		// Fill the entire image with maximum diff color.
		maxDiffColor := uint8ToColor(PixelDiffColor[deltaOffset(1024)])
		draw.Draw(resultImg, resultImg.Bounds(), &image.Uniform{maxDiffColor}, image.ZP, draw.Src)

		for x := 0; x < cmpWidth; x++ {
			for y := 0; y < cmpHeight; y++ {
				color1 := img1.At(x, y)
				color2 := img2.At(x, y)

				dc := diffColors(color1, color2, maxRGBADiffs)
				if dc == PixelMatchColor {
					numDiffPixels--
				}
				resultImg.Set(x, y, dc)
			}
		}
	}

	return &DiffMetrics{
		NumDiffPixels:    numDiffPixels,
		PixelDiffPercent: getPixelDiffPercent(numDiffPixels, totalPixels),
		MaxRGBADiffs:     maxRGBADiffs,
		DimDiffer:        (cmpWidth != resultWidth) || (cmpHeight != resultHeight)}, resultImg
}
