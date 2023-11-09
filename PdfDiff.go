package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/go-fitz"
	"github.com/phpdave11/gofpdf"
)

// Mutex to avoid race conditions when multiple goroutines access the same memory
var mutex = &sync.Mutex{}

// brightness calculates the brightness of a color using the luma formula.
func brightness(c color.Color) uint32 {
	const rCoeff = 299
	const gCoeff = 587
	const bCoeff = 114
	const scale = 1000 // Used to maintain precision in integer operations

	r, g, b, _ := c.RGBA()

	// Scale the r, g, and b values to fit into the 0-255 range
	r, g, b = r>>8, g>>8, b>>8

	// Calculate the brightness using integer arithmetic and the luma formula
	return (rCoeff*r + gCoeff*g + bCoeff*b) / scale
}


// worker is a function that will be run in a separate goroutine. It processes jobs from the jobs channel and sends a signal to the done channel when it finishes a job.
// It takes images from two PDF documents and compares them, creating a new image that highlights the differences.
func worker(id int, jobs <-chan int, done chan<- bool, doc1 *fitz.Document, doc2 *fitz.Document, mergeFlag *bool, offset int, startOffset int, totalOps int, sideBySideFlag *bool, verticalAlignFlag *bool) {
	for j := range jobs {
		var img1, img2 image.Image
		var err error

		// If we've reached the startOffset, create images for the pages from startOffset to startOffset+offset in file2
		if j == startOffset {
			for i := startOffset; i < startOffset+offset; i++ {
				if i < doc2.NumPage() {
					mutex.Lock()
					img, err := doc2.Image(i - 1)
					mutex.Unlock()
					if checkError(err) != nil {
						continue
					}
					imgPath := fmt.Sprintf("differences_%d.png", i)
					err = imaging.Save(img, imgPath)
					if checkError(err) != nil {
						continue
					}
				}
			}
		}

		// Extract the images from the PDFs or create a white image if the page does not exist
		if j < doc1.NumPage() {
			mutex.Lock()
			img1, err = doc1.Image(j)
			mutex.Unlock()
			if checkError(err) != nil {
				continue
			}
		} else {
			img1 = image.NewRGBA(image.Rect(0, 0, 595, 842)) // dimensions of an A4 page in points
		}

		pagToCompare := j
		if j >= startOffset {
			pagToCompare = j + offset
		}

		if pagToCompare < doc2.NumPage() {
			mutex.Lock()
			img2, err = doc2.Image(pagToCompare)
			mutex.Unlock()
			if checkError(err) != nil {
				continue
			}
		} else {
			img2 = image.NewRGBA(image.Rect(0, 0, 595, 842)) // dimensions of an A4 page in points
		}

		// Create an image to show the differences
		diffImg := image.NewRGBA(img1.Bounds())
		for y := 0; y < img1.Bounds().Dy(); y++ {
			for x := 0; x < img1.Bounds().Dx(); x++ {
				c1 := img1.At(x, y)
				c2 := img2.At(x, y)
				// Check if the pixels at the same position in both images are different
				if c1 != c2 {
					// If the pixels are different, color the pixel depending on which image has the brighter pixel
					// The brightness is calculated as the sum of the squares of the RGB components
					if brightness(c1) > brightness(c2) {
						// If the pixel in the first image is brighter, color the pixel in the difference image red
						diffImg.Set(x, y, color.RGBA{255, 0, 0, 255}) // red for image 1
					} else {
						// If the pixel in the second image is brighter, color the pixel in the difference image blue
						diffImg.Set(x, y, color.RGBA{0, 0, 255, 255}) // blue for image 2
					}
				} else {
					// If the pixels are the same, use the original pixel in the difference image
					diffImg.Set(x, y, c1)
				}
			}
		}

		// Save the difference image
		diffImgPath := fmt.Sprintf("differences_%d.png", j)
		if j >= startOffset {
			diffImgPath = fmt.Sprintf("differences_%d.png", j+offset)
		}
		err = imaging.Save(diffImg, diffImgPath)
		if checkError(err) != nil {
			continue
		}

		if *sideBySideFlag {
			var combinedWidth, combinedHeight int

			if *verticalAlignFlag {
				// For vertical alignment
				combinedWidth = max(img1.Bounds().Dx(), img2.Bounds().Dx())
				combinedHeight = img1.Bounds().Dy() + img2.Bounds().Dy()
			} else {
				// For horizontal alignment
				combinedWidth = img1.Bounds().Dx() + img2.Bounds().Dx()
				combinedHeight = max(img1.Bounds().Dy(), img2.Bounds().Dy())
			}

			combinedImg := image.NewRGBA(image.Rect(0, 0, combinedWidth, combinedHeight))

			// Copy img1 to combinedImg
			for y := 0; y < img1.Bounds().Dy(); y++ {
				for x := 0; x < img1.Bounds().Dx(); x++ {
					combinedImg.Set(x, y, img1.At(x, y))
				}
			}

			if *verticalAlignFlag {
				// Copy img2 to combinedImg for vertical alignment
				for y := 0; y < img2.Bounds().Dy(); y++ {
					for x := 0; x < img2.Bounds().Dx(); x++ {
						combinedImg.Set(x, y+img1.Bounds().Dy(), img2.At(x, y))
					}
				}
			} else {
				// Copy img2 to combinedImg for horizontal alignment
				for y := 0; y < img2.Bounds().Dy(); y++ {
					for x := 0; x < img2.Bounds().Dx(); x++ {
						combinedImg.Set(x+img1.Bounds().Dx(), y, img2.At(x, y))
					}
				}
			}

			// Save the combined image
			combinedImgPath := fmt.Sprintf("combined_%d.png", j)
			err = imaging.Save(combinedImg, combinedImgPath)
			if checkError(err) != nil {
				continue
			}
		}

		// Signal that the job is done
		done <- true
	}
}

func main() {
	// Define the flags
	mergeFlag := flag.Bool("merge", false, "merge the difference images into a single PDF")
	cleanFlag := flag.Bool("clean", false, "remove the difference images after processing")
	offsetFlag := flag.Int("offset", 0, "the number of pages to skip in the second PDF")
	startOffsetFlag := flag.Int("startoffset", 0, "the page of the first PDF to start the offset")
	orientationFlag := flag.String("orientation", "", "the orientation of the PDF (P for portrait, L for landscape)")
	printSizeFlag := flag.String("printsize", "A3", "Size of printed PDF A4,A3,A2...")
	outputFlag := flag.String("output", "differences.pdf", "the name of the output PDF file")
	workersFlag := flag.Int("workers", 0, "the number of workers to use. (Default: CPU Count)")
	sideBySideFlag := flag.Bool("sidebyside", false, "create a side-by-side comparison of the two PDFs")
	verticalAlignFlag := flag.Bool("verticalalign", false, "align the documents vertically in the combined image")

	// Parse the flags
	flag.Parse()

	// Check if the workers flag has been set
	if *workersFlag == 0 {
		*workersFlag = runtime.NumCPU()
	}
	// Check that two arguments have been passed
	if flag.NArg() != 2 {
		fmt.Println("Usage: [-merge] [-clean] [-printsize A4|A3|A2|A1|A0] [-offset n] [-startoffset n] [-orientation P|L] [-output output.pdf] [-workers n] <file1.pdf> <file2.pdf>")
		os.Exit(1)
	}

	// Get the paths of the PDF files from the command line arguments
	file1 := flag.Arg(0)
	file2 := flag.Arg(1)

	// Check if the files exist
	if _, err := os.Stat(file1); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: file %s does not exist\n", file1)
		os.Exit(1)
	}

	if _, err := os.Stat(file2); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: file %s does not exist\n", file2)
		os.Exit(1)
	}

	// Open the first PDF file
	doc1, err := fitz.New(file1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	// Ensure the document is closed after use
	defer doc1.Close()

	// Open the second PDF file
	doc2, err := fitz.New(file2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	// Ensure the document is closed after use
	defer doc2.Close()

	// Check that the offset and startoffset are valid
	if *offsetFlag < 0 || *offsetFlag >= doc2.NumPage() {
		fmt.Fprintf(os.Stderr, "Error: The offset is invalid. It should be between 0 and %d.\n", doc2.NumPage()-1)
		os.Exit(1)
	}
	if *startOffsetFlag < 0 || *startOffsetFlag >= doc1.NumPage() {
		fmt.Fprintf(os.Stderr, "Error: The startOffset is invalid. It should be between 0 and %d.\n", doc1.NumPage()-1)
		os.Exit(1)
	}
	// Check that the orientation is valid
	if *orientationFlag != "" && *orientationFlag != "P" && *orientationFlag != "L" {
		fmt.Fprintf(os.Stderr, "Error: The orientation is invalid. It should be either 'P' or 'L'.\n")
		os.Exit(1)
	}

	// Check that the print size is valid
	if *printSizeFlag != "A4" && *printSizeFlag != "A3" && *printSizeFlag != "A2" && *printSizeFlag != "A1" && *printSizeFlag != "A0" {
		fmt.Fprintf(os.Stderr, "Error: Invalid print size. It should be one of 'A4', 'A3', 'A2', 'A1', or 'A0'.\n")
		os.Exit(1)
	}

	// Calculate the total number of operations
	totalOps := max(doc1.NumPage(), doc2.NumPage())
	if *mergeFlag {
		totalOps++ // for merging the images into a PDF
	}
	if *cleanFlag {
		totalOps++ // for removing the images
	}

	// If the orientation has not been specified, set the orientation based on the dimensions of the first page
	if *orientationFlag == "" {
		img1, err := doc1.Image(0)
		if checkError(err) != nil {
			return
		}
		if img1.Bounds().Dx() > img1.Bounds().Dy() {
			*orientationFlag = "L"
		} else {
			*orientationFlag = "P"
		}
	}

	// Create a new PDF for the difference images
	pdf := gofpdf.New(*orientationFlag, "mm", *printSizeFlag, "")

	// Create a channel for the jobs
	jobs := make(chan int, max(doc1.NumPage(), doc2.NumPage()))

	// Create a channel to signal job completion
	done := make(chan bool)

	// Create the workers
	for w := 1; w <= *workersFlag; w++ {
		go func(id int) {
			worker(id, jobs, done, doc1, doc2, mergeFlag, *offsetFlag, *startOffsetFlag, totalOps, sideBySideFlag, verticalAlignFlag)
		}(w)
	}

	// Initialize the count of completed operations
	completedOps := 0

	// Iterate over all the pages of the PDFs
	for i := 0; i < max(doc1.NumPage(), doc2.NumPage()); i++ {
		// Send the job to the workers
		jobs <- i
	}

	// Close the jobs channel to signal that there are no more jobs to do
	close(jobs)

	// Wait for all jobs to be completed
	for i := 0; i < max(doc1.NumPage(), doc2.NumPage()); i++ {
		<-done
		// Update the count of completed operations and print the progress percentage
		completedOps++
		fmt.Printf("%.2f%% completed\n", float64(completedOps)/float64(totalOps)*100)
	}

	fmt.Printf("Merging difference images...")

	// Add the images to the PDF in the correct order
	if *mergeFlag {
		imgOptions := gofpdf.ImageOptions{
			ImageType:             "",
			ReadDpi:               true,
			AllowNegativePosition: true,
		}
		doc1Pages := doc1.NumPage()
		doc2Pages := doc2.NumPage()
		maxPages := max(doc1Pages+*offsetFlag, doc2Pages+*offsetFlag)

		pdfW, pdfH := pdf.GetPageSize()

		for i := 0; i < maxPages; i++ {
			pdf.AddPage()

			// Assuming each image has a unique path with index i
			diffImgPath := fmt.Sprintf("differences_%d.png", i)

			// Register each image inside the loop if they are not the same
			imgInfo := pdf.RegisterImageOptions(diffImgPath, imgOptions)
			imgW, imgH := imgInfo.Extent()
			scale := min(pdfW/imgW, pdfH/imgH)
			scaledImgW := imgW * scale
			scaledImgH := imgH * scale

			// Calculate the position of the image so that it is centered on the page
			x := (pdfW - scaledImgW) / 2
			y := (pdfH - scaledImgH) / 2

			// Add the image to the PDF
			pdf.ImageOptions(diffImgPath, x, y, scaledImgW, scaledImgH, false, imgOptions, 0, "")

			// Update and print the progress percentage less frequently to improve performance
			if i%(maxPages/10) == 0 || i == maxPages-1 { // Update every 10% or on the last image
				progress := float64(i+1) / float64(maxPages) * 100.0
				fmt.Printf("\rProgress: %.2f%%", progress)
			}
		}
		fmt.Println()

		// Save the PDF
		err := pdf.OutputFileAndClose(*outputFlag)
		if checkError(err) != nil {
			return
		}
		fmt.Printf("The difference images have been merged into %s\n", *outputFlag)

		// Update the count of completed operations and print the final message
		completedOps++
		fmt.Printf("The difference images have been merged into a PDF (100.00%% completed)\n")
	}

	if *sideBySideFlag {
		// Create a new PDF for the combined images
		pdf := gofpdf.New(*orientationFlag, "mm", *printSizeFlag, "")

		// Number of combined images to process
		numCombinedImages := max(doc1.NumPage()+*offsetFlag, doc2.NumPage()+*offsetFlag)

		// Loop through all combined images and add them to the PDF
		for i := 0; i < numCombinedImages; i++ {
			combinedImgPath := fmt.Sprintf("combined_%d.png", i)

			// Check if the image exists before trying to add it to the PDF
			if _, err := os.Stat(combinedImgPath); !os.IsNotExist(err) {
				imgOptions := gofpdf.ImageOptions{
					ImageType:             "",
					ReadDpi:               true,
					AllowNegativePosition: true,
				}
				imgInfo := pdf.RegisterImageOptions(combinedImgPath, imgOptions)

				// Convert the image dimensions from points to millimeters (assuming 72 dpi)
				imgWidthMM := imgInfo.Width() / 2.83465
				imgHeightMM := imgInfo.Height() / 2.83465

				// Add a new page with the exact size of the image
				pdf.AddPageFormat("P", gofpdf.SizeType{Wd: imgWidthMM, Ht: imgHeightMM})

				// Add the image to the PDF
				pdf.ImageOptions(combinedImgPath, 0, 0, imgWidthMM, imgHeightMM, false, imgOptions, 0, "")
			}
		}

		// Save the PDF
		outputCombinedPDF := filepath.Join(filepath.Dir(*outputFlag), "combined_"+filepath.Base(*outputFlag))
		err := pdf.OutputFileAndClose(outputCombinedPDF)
		if checkError(err) != nil {
			return
		}
		fmt.Printf("The combined images have been merged into %s\n", outputCombinedPDF)
	}

	if *cleanFlag {
		// Get the paths of the difference images.
		var differenceImagePaths []string
		for i := 0; i < max(doc1.NumPage()+*offsetFlag, doc2.NumPage()+*offsetFlag); i++ {
			differenceImagePaths = append(differenceImagePaths, fmt.Sprintf("differences_%d.png", i))
			if *sideBySideFlag {
				differenceImagePaths = append(differenceImagePaths, fmt.Sprintf("combined_%d.png", i))
			}
		}

		// Remove the images.
		for _, imagePath := range differenceImagePaths {
			err := os.Remove(imagePath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error removing image: %v\n", err)
			}
		}

		fmt.Println("The images have been removed")

		// Update the count of completed operations and print the progress percentage
		completedOps++
		fmt.Printf("The images have been removed (%.2f%% completed)\n", float64(completedOps)/float64(totalOps)*100)
	}
}

// min returns the smaller of two float64 numbers.
func min(a, b float64) float64 {
	return math.Min(a, b)
}

// max returns the larger of two int numbers.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
// checkError prints an error message and returns the error if it is not nil.
func checkError(err error) error {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}
	return nil
}
