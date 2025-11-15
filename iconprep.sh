#! /bin/sh

# prepare icons for the project
# Usage: iconprep.sh <png_filename>
# Example: iconprep.sh KrankyBearCowboyBrown.png

if [ $# -eq 0 ]; then
    echo "Usage: $0 <png_filename>"
    echo "Example: $0 KrankyBearCowboyBrown.png"
    exit 1
fi

PNG_NAME="$1"
PNG_PATH="Resources/Images/${PNG_NAME}"

# Check if PNG file exists
if [ ! -f "$PNG_PATH" ]; then
    echo "Error: PNG file not found: $PNG_PATH"
    exit 1
fi

# Extract base name without extension for .ico filename
BASE_NAME="${PNG_NAME%.png}"
ICO_PATH="Resources/Images/${BASE_NAME}.ico"

echo "Processing icon: $PNG_NAME"
echo "PNG path: $PNG_PATH"
echo "ICO path: $ICO_PATH"

# Create icons from PNG
~/bin/img2icons -image "$PNG_PATH"
echo "Icons created in Resources/Images"
ls -lrt Resources/Images/${BASE_NAME}.ic*

# NOTE: The syso files created below are from the old rsrc tool.
# If you're using go-winres (which reads winres.json), these syso files will conflict
# and cause the wrong icon to be embedded. The compile-windows.ps1 script will
# automatically remove these files, but you may want to remove them manually:
#   rm -f rsrc_windows_*.syso
# The go-winres tool will create its own syso files based on winres.json.

# Install the rsrc bits for Windows (legacy - only needed if not using go-winres)
go install github.com/akavel/rsrc@latest

# Create the syso files for Windows (legacy - conflicts with go-winres)
#rsrc -arch 386 -ico "$ICO_PATH" -o rsrc_windows_386.syso
#rsrc -arch amd64 -ico "$ICO_PATH" -o rsrc_windows_amd64.syso
echo "syso files created (NOTE: These will conflict with go-winres - remove before compiling)"
ls -lrt rsrc_windows_*.syso