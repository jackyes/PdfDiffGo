PDF Diff Tool

PDF Diff Tool is a Go-based application that allows you to compare two PDF files page by page, highlighting any differences between them. It also provides the option to merge the difference images into a single PDF.
Features

    Compare two PDF files page by page.
    Highlight differences between the two PDFs.
    Merge the difference images into a single PDF (optional).
    Remove the difference images after processing (optional).
    Customize the number of workers to use for processing.
    Customize the orientation and print size of the output PDF.

Usage:

    PdfDiffGo [-merge] [-clean] [-printsize A4|A3|A2|A1|A0] [-offset n] [-start n] [-orientation P|L] [-output output.pdf] [-workers n] <file1.pdf> <file2.pdf>

Flags

    -merge: Merge the difference images into a single PDF.
    -clean: Remove the difference images after processing.
    -printsize: Size of printed PDF (A4, A3, A2, A1, A0).
    -offset: The number of pages to skip in the second PDF.
    -start: The page of the first PDF to start the offset.
    -orientation: The orientation of the PDF (P for portrait, L for landscape).
    -output: The name of the output PDF file.
    -workers: The number of workers to use for processing.

Usage example  

    PdfDiffGo -merge -clean -output /path/to/save/Diff.pdf /path/to/Pdf1.pdf /path/to/Pdf2.pdf
