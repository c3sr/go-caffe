package main

import (
	_ "bufio"
	"context"
	"fmt"
	"reflect"
	"image"
	_ "os"
	"path/filepath"
	_ "sort"

	"github.com/anthonynsimon/bild/imgio"
	"github.com/anthonynsimon/bild/transform"
	_ "github.com/k0kubun/pp"

	"github.com/rai-project/config"
	_ "github.com/rai-project/dlframework"
	_ "github.com/rai-project/dlframework/framework/feature"
	"github.com/rai-project/dlframework/framework/options"
	_ "github.com/rai-project/downloadmanager"
	"github.com/rai-project/go-caffe"
	cupti "github.com/rai-project/go-cupti"
	nvidiasmi "github.com/rai-project/nvidia-smi"
	"github.com/rai-project/tracer"
	_ "github.com/rai-project/tracer/all"
	"github.com/rai-project/tracer/ctimer"
)

var (
	batchSize   = 1
	model = "SSD_VOC0712"
)

// convert go Image to 1-dim array
func cvtImageTo1DArray(src image.Image, mean []float32) ([]float32, error) {
	if src == nil {
		return nil, fmt.Errorf("src image nil")
	}

	b := src.Bounds()
	h := b.Max.Y - b.Min.Y // image height
	w := b.Max.X - b.Min.X // image width

	res := make([]float32, 3*h*w)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := src.At(x+b.Min.X, y+b.Min.Y).RGBA()
			//what are we doing to RGB below??
			res[y*w+x] = float32(b>>8) - mean[0]
			res[w*h+y*w+x] = float32(g>>8) - mean[1]
			res[2*w*h+y*w+x] = float32(r>>8) - mean[2]
		}
	}
	return res, nil
}

func main() {
	defer tracer.Close()

	dir, _ := filepath.Abs("../tmp")
	dir = filepath.Join(dir, model)
	graph := filepath.Join(dir, "deploy.prototxt")
	weights := filepath.Join(dir, "VGG_VOC0712_SSD_300x300_iter_120000.caffemodel")
	labelmap := filepath.Join(dir, "labelmap_voc.prototxt")

	 _ = labelmap


	imgDir, _ := filepath.Abs("../_fixtures")
	imagePath := filepath.Join(imgDir, "fish-bike.jpg")


	img, err := imgio.Open(imagePath)
	if err != nil {
		panic(err)
	}

	var input []float32
	for ii := 0; ii < batchSize; ii++ {
		resized := transform.Resize(img, 300, 300, transform.Linear)
		res, err := cvtImageTo1DArray(resized, []float32{104,117,123}) // BGR
		if err != nil {
			panic(err)
		}
		input = append(input, res...)
	}

	opts := options.New()

	device := options.CPU_DEVICE
	if nvidiasmi.HasGPU {
		caffe.SetUseGPU()
		device = options.CUDA_DEVICE
	} else {
		caffe.SetUseCPU()
	}

	ctx := context.Background()

	span, ctx := tracer.StartSpanFromContext(ctx, tracer.FULL_TRACE, "caffe_batch")
	defer span.Finish()

	predictor, err := caffe.New(
		ctx,
		options.WithOptions(opts),
		options.Device(device, 0),
		options.Graph([]byte(graph)),
		options.Weights([]byte(weights)),
		options.BatchSize(batchSize))
	if err != nil {
		panic(err)
	}
	defer predictor.Close()

	err = predictor.Predict(ctx, input)
	if err != nil {
		panic(err)
	}

	var cu *cupti.CUPTI
	if nvidiasmi.HasGPU {
		cu, err = cupti.New(cupti.Context(ctx))
		if err != nil {
			panic(err)
		}
	}

	predictor.StartProfiling("predict", "")

	err = predictor.Predict(ctx, input)
	if err != nil {
		panic(err)
	}

	predictor.EndProfiling()

	if nvidiasmi.HasGPU {
		cu.Wait()
		cu.Close()
	}

	profBuffer, err := predictor.ReadProfile()
	if err != nil {
		panic(err)
	}
	predictor.DisableProfiling()

	t, err := ctimer.New(profBuffer)
	if err != nil {
		panic(err)
	}
	t.Publish(ctx, tracer.FRAMEWORK_TRACE)

	output, err := predictor.ReadPredictionOutput(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println(reflect.TypeOf(output))
	fmt.Printf("%v", output)




	// //Postprocessing

	// var labels []string
	// f, err := os.Open(synset)
	// if err != nil {
	// 	panic(err)
	// }
	// defer f.Close()
	// scanner := bufio.NewScanner(f)
	// for scanner.Scan() {
	// 	line := scanner.Text()
	// 	labels = append(labels, line)
	// }

	// features := make([]dlframework.Features, batchSize)
	// featuresLen := len(output) / batchSize

	// for ii := 0; ii < batchSize; ii++ {
	// 	rprobs := make([]*dlframework.Feature, featuresLen)
	// 	for jj := 0; jj < featuresLen; jj++ {
	// 		rprobs[jj] = feature.New(
	// 			feature.ClassificationIndex(int32(jj)),
	// 			feature.ClassificationLabel(labels[jj]),
	// 			feature.Probability(output[ii*featuresLen+jj]),
	// 		)
	// 	}
	// 	sort.Sort(dlframework.Features(rprobs))
	// 	features[ii] = rprobs
	// }

	// if true {
	// 	for i := 0; i < 1; i++ {
	// 		results := features[i]
	// 		top1 := results[0]
	// 		pp.Println(top1.Probability)
	// 		pp.Println(top1.GetClassification().GetLabel())
	// 	}
	// } else {
	// 	_ = features
	// }
}

func init() {
	config.Init(
		config.AppName("carml"),
		config.VerboseMode(true),
		config.DebugMode(true),
	)
}
