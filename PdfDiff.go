package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/go-fitz"
	"github.com/jung-kurt/gofpdf"
)

func brightness(c color.Color) uint32 {
	r, g, b, _ := c.RGBA()
	return r*r + g*g + b*b // calculate brightness as the sum of the squares of the RGB components
}

func worker(id int, jobs <-chan int, done chan<- bool, doc1 *fitz.Document, doc2 *fitz.Document, mergeFlag *bool, totalOps int) {
	for j := range jobs {
		var img1, img2 image.Image
		var err error

		// Extract the images from the PDFs or create a white image if the page does not exist
		if j < doc1.NumPage() {
			img1, err = doc1.Image(j)
			checkError(err)
		} else {
			img1 = image.NewRGBA(image.Rect(0, 0, 595, 842)) // dimensions of an A4 page in points
		}

		if j < doc2.NumPage() {
			img2, err = doc2.Image(j)
			checkError(err)
		} else {
			img2 = image.NewRGBA(image.Rect(0, 0, 595, 842)) // dimensions of an A4 page in points
		}

		// Create an image to show the differences
		diffImg := image.NewRGBA(img1.Bounds())
		for y := 0; y < img1.Bounds().Dy(); y++ {
			for x := 0; x < img1.Bounds().Dx(); x++ {
				c1 := img1.At(x, y)
				c2 := img2.At(x, y)
				if c1 != c2 {
					// If the pixels are different, color the pixel depending on which image has the brighter pixel
					if brightness(c1) > brightness(c2) {
						diffImg.Set(x, y, color.RGBA{255, 0, 0, 255}) // red for image 1
					} else {
						diffImg.Set(x, y, color.RGBA{0, 0, 255, 255}) // blue for image 2
					}
				} else {
					// Otherwise, use the original pixel
					diffImg.Set(x, y, c1)
				}
			}
		}

		// Save the difference image
		diffImgPath := fmt.Sprintf("differences_%d.png", j)
		err = imaging.Save(diffImg, diffImgPath)
		checkError(err)

		// Signal that the job is done
		done <- true
	}
}

func main() {
	// Define the flags
	mergeFlag := flag.Bool("merge", false, "merge the difference images into a single PDF")
	cleanFlag := flag.Bool("clean", false, "remove the difference images after processing")
	offsetFlag := flag.Int("offset", 0, "the number of pages to skip in the second PDF")
	startFlag := flag.Int("start", 0, "the page of the first PDF to start the offset")
	orientationFlag := flag.String("orientation", "", "the orientation of the PDF (P for portrait, L for landscape)")
	printSizeFlag := flag.String("printsize", "A3", "Size of printed PDF A4,A3,A2...)")
	outputFlag := flag.String("output", "differences.pdf", "the name of the output PDF file")
	workersFlag := flag.Int("workers", 30, "the number of workers to use")

	// Parse the flags
	flag.Parse()

	// Check that two arguments have been passed
	if flag.NArg() != 2 {
		fmt.Println("Usage: [-merge] [-clean] [-printsize A4|A3|A2|A1|A0] [-offset n] [-start n] [-orientation P|L] [-output output.pdf] [-workers n] <file1.pdf> <file2.pdf>")
		os.Exit(1)
	}

	// Get the paths of the PDF files from the command line arguments
	file1 := flag.Arg(0)
	file2 := flag.Arg(1)

	// Open the PDF files
	doc1, err := fitz.New(file1)
	checkError(err)
	defer doc1.Close()

	doc2, err := fitz.New(file2)
	checkError(err)
	defer doc2.Close()

	// Check that the offset and start are valid
	if *offsetFlag < 0 || *offsetFlag >= doc2.NumPage() {
		panic("The offset is invalid")
	}
	if *startFlag < 0 || *startFlag >= doc1.NumPage() {
		panic("The start is invalid")
	}

	// Check that the orientation is valid
	if *orientationFlag != "" && *orientationFlag != "P" && *orientationFlag != "L" {
		panic("The orientation is invalid")
	}
	// Check that the print size is valid
	if *printSizeFlag != "A4" && *printSizeFlag != "A3" && *printSizeFlag != "A2" && *printSizeFlag != "A1" && *printSizeFlag != "A0" {
		panic("Invalid print size")
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
		checkError(err)
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
			worker(id, jobs, done, doc1, doc2, mergeFlag, totalOps)
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

	// Add the images to the PDF in the correct order
	if *mergeFlag {
		for i := 0; i < max(doc1.NumPage(), doc2.NumPage()); i++ {
			pdf.AddPage()
			// Calculate the dimensions of the image so that it fits the PDF page
			imgOptions := gofpdf.ImageOptions{
				ImageType:             "",
				ReadDpi:               true,
				AllowNegativePosition: true,
			}
			diffImgPath := fmt.Sprintf("differences_%d.png", i)
			imgInfo := pdf.RegisterImageOptions(diffImgPath, imgOptions)
			imgW, imgH := imgInfo.Extent()
			pdfW, pdfH := pdf.GetPageSize()
			scale := min(pdfW/imgW, pdfH/imgH)
			imgW *= scale
			imgH *= scale
			// Calculate the position of the image so that it is centered on the page
			x := (pdfW - imgW) / 2
			y := (pdfH - imgH) / 2
			// Add the image to the PDF
			pdf.ImageOptions(diffImgPath, x, y, imgW, imgH, false, imgOptions, 0, "")
		}
		// Save the PDF
		err = pdf.OutputFileAndClose(*outputFlag)
		checkError(err)
		fmt.Printf("The difference images have been merged into %s\n", *outputFlag)

		// Update the count of completed operations and print the progress percentage
		completedOps++
		fmt.Printf("The difference images have been merged into a PDF (%.2f%% completed)\n", float64(completedOps)/float64(totalOps)*100)
	}

	// Remove the difference images
	if *cleanFlag {
		files, err := filepath.Glob("differences_*.png")
		checkError(err)
		for _, f := range files {
			if err := os.Remove(f); err != nil {
				checkError(err)
			}
		}
		fmt.Println("The difference images have been removed")

		// Update the count of completed operations and print the progress percentage
		completedOps++
		fmt.Printf("The difference images have been removed (%.2f%% completed)\n", float64(completedOps)/float64(totalOps)*100)
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// checkError prints an error message and terminates the program if err is not nil.
func checkError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
