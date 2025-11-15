#!/usr/bin/env bash

set -euo pipefail

# Script to rename the application template
# Usage: ./rename-app.sh "New App Name" "icon-filename.png"

usage() {
    cat <<EOF
Usage: $0 "New Application Name" "icon-filename.png"

Arguments:
  New Application Name  - The new name for the application (e.g., "KrankyBear AwesomeApp")
  icon-filename.png     - The icon file name located in Resources/Images/ (e.g., "KrankyBearFunnyHat.png")

This script will:
  1. Replace "KrankyBear Template" with the new application name in all text files
  2. Replace template-mac*, template-linux*, template-windows* with new-name-mac*, etc.
  3. Rename KrankyBearTemplate.app directory to {NewAppName}.app
  4. Update files in winres/* and Inno/* directories
  5. Update references to use the new application name

Example:
  $0 "KrankyBear AwesomeApp" "KrankyBearFunnyHat.png"
EOF
    exit 1
}

# Check arguments
if [[ $# -ne 2 ]]
then
    echo "Error: Expected 2 arguments, got $#" >&2
    usage
fi

NEW_APP_NAME="$1"
ICON_FILE="$2"

# Validate icon file exists
ICON_PATH="Resources/Images/$ICON_FILE"
if [[ ! -f "$ICON_PATH" ]]
then
    echo "Error: Icon file not found: $ICON_PATH" >&2
    echo "Please ensure the icon file exists in Resources/Images/" >&2
    exit 1
fi

# Convert app name to binary name format (lowercase, spaces/hyphens to hyphens)
# Remove special characters and convert to lowercase with hyphens
BINARY_NAME=$(echo "$NEW_APP_NAME" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g' | sed 's/--*/-/g' | sed 's/^-\|-$//g')

# Create short binary name (remove "krankybear" prefix if present, for cleaner binary names)
SHORT_BINARY_NAME=$(echo "$BINARY_NAME" | sed 's/^krankybear-//' | sed 's/^kranky-bear-//')
# If nothing left after removing prefix, use the full binary name
if [[ -z "$SHORT_BINARY_NAME" ]]
then
    SHORT_BINARY_NAME="$BINARY_NAME"
fi

# Convert app name to identifier format (PascalCase, no spaces)
# If the name starts with "KrankyBear", preserve the case of the part after it
if echo "$NEW_APP_NAME" | grep -qiE "^KrankyBear[[:space:]]"
then
    # Extract the part after "KrankyBear " (with space) - preserve case
    SUFFIX=$(echo "$NEW_APP_NAME" | sed -E 's/^[Kk]ranky[Bb]ear[[:space:]]+//')
    if [[ -n "$SUFFIX" ]]
    then
        # Remove spaces and special characters, but preserve case
        SUFFIX_CLEAN=$(echo "$SUFFIX" | sed 's/[^a-zA-Z0-9]//g')
        IDENTIFIER_NAME="KrankyBear${SUFFIX_CLEAN}"
    else
        # Fallback to full conversion if no suffix found
        if command -v python3 &> /dev/null
        then
            IDENTIFIER_NAME=$(python3 -c "import sys; s=sys.stdin.read().strip(); print(''.join(word.capitalize() for word in s.replace('-',' ').replace('_',' ').split() if word.isalnum() or any(c.isalnum() for c in word)))" <<< "$NEW_APP_NAME")
        else
            IDENTIFIER_NAME=$(echo "$NEW_APP_NAME" | sed 's/[^a-zA-Z0-9]/ /g' | sed 's/\b\(.\)/\u\1/g' | sed 's/ //g')
        fi
    fi
elif echo "$NEW_APP_NAME" | grep -qiE "^KrankyBear[^[:space:]]"
then
    # Extract the part after "KrankyBear" (without space, like "KrankyBearTemplate") - preserve case
    SUFFIX=$(echo "$NEW_APP_NAME" | sed -E 's/^[Kk]ranky[Bb]ear//')
    if [[ -n "$SUFFIX" ]]
    then
        # Remove spaces and special characters, but preserve case
        SUFFIX_CLEAN=$(echo "$SUFFIX" | sed 's/[^a-zA-Z0-9]//g')
        IDENTIFIER_NAME="KrankyBear${SUFFIX_CLEAN}"
    else
        # Fallback to full conversion if no suffix found
        if command -v python3 &> /dev/null
        then
            IDENTIFIER_NAME=$(python3 -c "import sys; s=sys.stdin.read().strip(); print(''.join(word.capitalize() for word in s.replace('-',' ').replace('_',' ').split() if word.isalnum() or any(c.isalnum() for c in word)))" <<< "$NEW_APP_NAME")
        else
            IDENTIFIER_NAME=$(echo "$NEW_APP_NAME" | sed 's/[^a-zA-Z0-9]/ /g' | sed 's/\b\(.\)/\u\1/g' | sed 's/ //g')
        fi
    fi
else
    # Use Python for reliable PascalCase conversion
    if command -v python3 &> /dev/null
    then
        IDENTIFIER_NAME=$(python3 -c "import sys; s=sys.stdin.read().strip(); print(''.join(word.capitalize() for word in s.replace('-',' ').replace('_',' ').split() if word.isalnum() or any(c.isalnum() for c in word)))" <<< "$NEW_APP_NAME")
    else
        # Fallback: simple capitalization (may not be perfect)
        IDENTIFIER_NAME=$(echo "$NEW_APP_NAME" | sed 's/[^a-zA-Z0-9]/ /g' | sed 's/\b\(.\)/\u\1/g' | sed 's/ //g')
    fi
fi

if [[ -z "$BINARY_NAME" ]]
then
    echo "Error: Could not generate valid binary name from '$NEW_APP_NAME'" >&2
    exit 1
fi

if [[ -z "$IDENTIFIER_NAME" ]]
then
    echo "Error: Could not generate valid identifier name from '$NEW_APP_NAME'" >&2
    exit 1
fi

echo "=========================================="
echo "Application Rename Script"
echo "=========================================="
echo "New Application Name: $NEW_APP_NAME"
echo "Binary Name Format: $BINARY_NAME"
echo "Short Binary Name: $SHORT_BINARY_NAME"
echo "Identifier Format: $IDENTIFIER_NAME"
echo "Icon File: $ICON_FILE"
echo "=========================================="
echo ""

# Function to find text files (excluding binary files, git, vendor, etc.)
# Note: We include winres/* and Inno/* explicitly, and exclude the .app directory
# Exclude rename-app.sh itself to avoid modifying the script
find_text_files() {
    find . -type f \
        -not -path "./.git/*" \
        -not -path "./vendor/*" \
        -not -path "./bin/*" \
        -not -path "./installers/*" \
        -not -path "./KrankyBearTemplate.app/*" \
        -not -path "./${IDENTIFIER_NAME}.app/*" \
        -not -path "./.idea/*" \
        -not -path "./.vscode/*" \
        -not -name "rename-app.sh" \
        -not -name "*.exe" \
        -not -name "*.dll" \
        -not -name "*.so" \
        -not -name "*.dylib" \
        -not -name "*.icns" \
        -not -name "*.ico" \
        -not -name "*.png" \
        -not -name "*.jpg" \
        -not -name "*.jpeg" \
        -not -name "*.gif" \
        -not -name "*.mp3" \
        -not -name "*.wav" \
        -not -name "*.syso" \
        -not -name "*.app" \
        -not -name ".DS_Store" \
        -not -name "*.swp" \
        -not -name "*.swo" \
        -not -name "*~" \
        | grep -v "^\./\." \
        | sort
}

# Count files to be processed
FILE_COUNT=$(find_text_files | wc -l | tr -d ' ')
echo "Found $FILE_COUNT text files to process"
echo ""

# Ask for confirmation
read -p "Continue with rename? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]
then
    echo "Aborted."
    exit 0
fi

echo ""
echo "Processing files..."

# Track changes
CHANGED_FILES=0
TOTAL_REPLACEMENTS=0

# Process each file
while IFS= read -r file
do
    # Skip if file doesn't exist or is not readable
    [[ ! -f "$file" ]] && continue
    
    # Check if file contains text (skip binary files)
    if ! file "$file" | grep -qE "(text|ASCII|UTF-8|empty)"
    then
        continue
    fi
    
    # Create backup
    cp "$file" "$file.bak"
    
    # Count replacements before making changes
    REPLACEMENTS=0
    COUNT=$(grep -o "KrankyBear Template" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    COUNT=$(grep -o "KrankyBearTemplate" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    # Count "Template" in git repository URLs
    COUNT=$(grep -oE "(github\.com|git\.|\.git|/)[^/]*Template" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    # Count "module template" in go.mod
    if [[ "$(basename "$file")" == "go.mod" ]] && grep -qE "^module template" "$file.bak" 2>/dev/null
    then
        REPLACEMENTS=$((REPLACEMENTS + 1))
    fi
    
    COUNT=$(grep -oE "template-mac[^[:space:]]*" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    COUNT=$(grep -oE "template-macos[^[:space:]]*" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    COUNT=$(grep -oE "template-linux[^[:space:]]*" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    COUNT=$(grep -oE "template-windows[^[:space:]]*" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    # Count standalone "template" references (not template-windows, template-linux, etc.)
    COUNT=$(grep -oE "(bin/|bin\\|Path.*['\"]|Filter.*['\"])template[^-]" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    COUNT=$(grep -oE "'template'|\"template\"|\*template\*" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    # Count "-o template" in build commands (prepare-deps files)
    if [[ "$(basename "$file")" == "prepare-deps.sh" ]] || [[ "$(basename "$file")" == "prepare-deps.ps1" ]]
    then
        COUNT=$(grep -oE "-o template|-o [a-z]*\.exe" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
        REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    fi
    
    # Count path variables and binary references in sync files
    if [[ "$(basename "$file")" =~ ^sync2.*\.sh$ ]]
    then
        COUNT=$(grep -oE "KrankyBearTemplate|template-linux|template-windows\.exe|C:\\\\Allan\\\\Source\\\\go\\\\KrankyBearTemplate" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
        REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    fi
    
    # Count "krankybeartemplate" (lowercase package name) and "template" in variable assignments
    COUNT=$(grep -oE "krankybeartemplate|NAME.*KrankyBearTemplate|REPLACES.*krankybeartemplate|BIN_NAME.*KrankyBearTemplate" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    COUNT=$(grep -oE "ReleaseNotes-template|/opt/local/bin/template|STAGING_DIR/template" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    # Count icon file references that need updating
    COUNT=$(grep -oE "KrankyBear[^[:space:]]*\.(png|ico|icns)" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    # Skip if no replacements needed
    if [[ $REPLACEMENTS -eq 0 ]]
    then
        rm "$file.bak"
        continue
    fi
    
    # Replace "KrankyBear Template" with new app name
    if grep -q "KrankyBear Template" "$file.bak" 2>/dev/null
    then
        sed -i '' "s/KrankyBear Template/$NEW_APP_NAME/g" "$file"
    fi
    
    # Replace "KrankyBearTemplate" (no space) with identifier name
    if grep -q "KrankyBearTemplate" "$file.bak" 2>/dev/null
    then
        sed -i '' "s/KrankyBearTemplate/$IDENTIFIER_NAME/g" "$file"
    fi
    
    # Replace "module template" in go.mod
    if [[ "$(basename "$file")" == "go.mod" ]] && grep -qE "^module template" "$file.bak" 2>/dev/null
    then
        sed -i '' "s|^module template|module ${SHORT_BINARY_NAME}|g" "$file"
    fi
    
    # Replace "Template" in git repository URLs (e.g., github.com:amarillier/Template.git)
    # Only replace if it's clearly a repository name, not part of a larger word
    if grep -qE "(github\.com|git\.|\.git|/)[^/]*Template" "$file.bak" 2>/dev/null
    then
        # Replace Template in git URLs - be careful to only match repository names
        sed -i '' "s|github\.com:amarillier/Template\.git|github.com:amarillier/${IDENTIFIER_NAME}.git|g" "$file"
        sed -i '' "s|git\.corp\.tanium\.com:[^/]*/Template\.git|git.corp.tanium.com:ivan-marillier/${IDENTIFIER_NAME}.git|g" "$file"
        sed -i '' "s|/Template\.git|/${IDENTIFIER_NAME}.git|g" "$file"
        sed -i '' "s|/Template\"|/${IDENTIFIER_NAME}\"|g" "$file"
        sed -i '' "s|/Template'|/${IDENTIFIER_NAME}'|g" "$file"
    fi
    
    # Replace template-macos* patterns first (longer match)
    if grep -qE "template-macos" "$file.bak" 2>/dev/null
    then
        sed -i '' "s/template-macos/${SHORT_BINARY_NAME}-macos/g" "$file"
    fi
    
    # Replace template-mac* patterns (shorter match, after macos)
    if grep -qE "template-mac[^o]" "$file.bak" 2>/dev/null
    then
        sed -i '' "s/template-mac\([^os]\)/${SHORT_BINARY_NAME}-mac\1/g" "$file"
    fi
    
    # Replace template-linux* patterns
    if grep -qE "template-linux" "$file.bak" 2>/dev/null
    then
        sed -i '' "s/template-linux/${SHORT_BINARY_NAME}-linux/g" "$file"
    fi
    
    # Replace template-windows* patterns
    if grep -qE "template-windows" "$file.bak" 2>/dev/null
    then
        sed -i '' "s/template-windows/${SHORT_BINARY_NAME}-windows/g" "$file"
    fi
    
    # Replace standalone "template" (without platform suffix) - handle carefully to avoid false matches
    # Only replace if it's clearly a binary name reference (in paths, variables, etc.)
    if grep -qE "(bin/|bin\\|Path.*['\"]|Filter.*['\"])template[^-]" "$file.bak" 2>/dev/null || grep -qE "'template'|\"template\"|\*template\*|bin/template[[:space:]]|bin/template$" "$file.bak" 2>/dev/null
    then
        # Replace template in paths like bin/template (not template-windows, template-linux, etc.)
        # First handle end of line case
        sed -i '' "s|bin/template$|bin/${SHORT_BINARY_NAME}|g" "$file"
        # Then handle followed by space or other non-hyphen characters
        sed -i '' "s|bin/template\([^-]\)|bin/${SHORT_BINARY_NAME}\1|g" "$file"
        # Replace template in PowerShell paths like bin\template (backslash)
        sed -i '' "s|bin\\\\template$|bin\\\\${SHORT_BINARY_NAME}|g" "$file"
        sed -i '' "s|bin\\\\template\([^-]\)|bin\\\\${SHORT_BINARY_NAME}\1|g" "$file"
        # Replace template in quotes or single quotes (standalone, not part of template-windows)
        sed -i '' "s|'template'|'${SHORT_BINARY_NAME}'|g" "$file"
        sed -i '' "s|\"template\"|\"${SHORT_BINARY_NAME}\"|g" "$file"
        # Replace template in filter patterns like *template* (PowerShell Filter parameter)
        sed -i '' "s|\*template\*|\*${SHORT_BINARY_NAME}\*|g" "$file"
    fi
    
    # Replace build command output flags in prepare-deps files (do this BEFORE global krankybeartemplate replacement)
    if [[ "$(basename "$file")" == "prepare-deps.sh" ]] || [[ "$(basename "$file")" == "prepare-deps.ps1" ]]
    then
        # Replace "-o template" in go build commands
        sed -i '' "s|-o template|-o ${SHORT_BINARY_NAME}|g" "$file"
        # Replace "-o template.exe" or "-o launchpad.exe" in PowerShell file
        if [[ "$(basename "$file")" == "prepare-deps.ps1" ]]
        then
            sed -i '' "s|-o [a-z]*\.exe|-o ${SHORT_BINARY_NAME}.exe|g" "$file"
        fi
    fi
    
    # Replace path variables and binary references in sync files (do this BEFORE global krankybeartemplate replacement)
    if [[ "$(basename "$file")" =~ ^sync2.*\.sh$ ]]
    then
        # Replace path variables containing KrankyBearTemplate
        sed -i '' "s|UBUNTU_PATH=\"/home/\${UBUNTU_USER}/KrankyBearTemplate\"|UBUNTU_PATH=\"/home/\${UBUNTU_USER}/${IDENTIFIER_NAME}\"|g" "$file"
        sed -i '' "s|MINT_PATH=\"/home/\${MINT_USER}/KrankyBearTemplate\"|MINT_PATH=\"/home/\${MINT_USER}/${IDENTIFIER_NAME}\"|g" "$file"
        sed -i '' "s|WINDOWS_SHARE=\"\$HOME/AllanMWin/Allan/Source/go/KrankyBearTemplate\"|WINDOWS_SHARE=\"\$HOME/AllanMWin/Allan/Source/go/${IDENTIFIER_NAME}\"|g" "$file"
        # Replace Windows path references in echo/instructions
        sed -i '' "s|C:\\\\Allan\\\\Source\\\\go\\\\KrankyBearTemplate|C:\\\\Allan\\\\Source\\\\go\\\\${IDENTIFIER_NAME}|g" "$file"
        # Replace template-linux references
        sed -i '' "s|template-linux|${SHORT_BINARY_NAME}-linux|g" "$file"
        # Replace template-windows.exe references
        sed -i '' "s|template-windows\.exe|${SHORT_BINARY_NAME}-windows.exe|g" "$file"
    fi
    
    # Replace variable defaults and assignments in package.sh (do this BEFORE global krankybeartemplate replacement)
    if [[ "$(basename "$file")" == "package.sh" ]]
    then
        # Replace NAME default: NAME=${NAME:-KrankyBearTemplate}
        sed -i '' "s|NAME=\${NAME:-KrankyBearTemplate}|NAME=\${NAME:-${IDENTIFIER_NAME}}|g" "$file"
        # Replace REPLACES default: REPLACES=${REPLACES:-"krankybeartemplate"}
        sed -i '' "s|REPLACES=\${REPLACES:-\"krankybeartemplate\"}|REPLACES=\${REPLACES:-\"${SHORT_BINARY_NAME}\"}|g" "$file"
        # Replace BIN_NAME assignment: BIN_NAME="KrankyBearTemplate"
        sed -i '' "s|BIN_NAME=\"KrankyBearTemplate\"|BIN_NAME=\"${IDENTIFIER_NAME}\"|g" "$file"
        # Replace symlink targets: ln -s ... template (do these before global replacement)
        sed -i '' "s|ln -s \"\$BIN_NAME\" \"\$APP_BUNDLE/Contents/MacOS/template\"|ln -s \"\$BIN_NAME\" \"\$APP_BUNDLE/Contents/MacOS/${SHORT_BINARY_NAME}\"|g" "$file"
        sed -i '' "s|ln -s \"krankybeartemplate\" \"\$STAGING_DIR/template\"|ln -s \"${SHORT_BINARY_NAME}\" \"\$STAGING_DIR/${SHORT_BINARY_NAME}\"|g" "$file"
        # Replace ReleaseNotes-template.txt
        sed -i '' "s|ReleaseNotes-template\.txt|ReleaseNotes-${SHORT_BINARY_NAME}.txt|g" "$file"
        # Replace /opt/local/bin/template paths
        sed -i '' "s|/opt/local/bin/template|/opt/local/bin/${SHORT_BINARY_NAME}|g" "$file"
        # Replace STAGING_DIR/template
        sed -i '' "s|\$STAGING_DIR/template|\$STAGING_DIR/${SHORT_BINARY_NAME}|g" "$file"
        sed -i '' "s|STAGING_DIR/template|STAGING_DIR/${SHORT_BINARY_NAME}|g" "$file"
        # Replace SRC_SYMLINK references
        sed -i '' "s|SRC_SYMLINK=\"\$STAGING_DIR/template\"|SRC_SYMLINK=\"\$STAGING_DIR/${SHORT_BINARY_NAME}\"|g" "$file"
        # Replace description text
        sed -i '' "s|KrankyBear Template - A cross-platform GUI Template application|${NEW_APP_NAME} - A cross-platform GUI application|g" "$file"
        # Replace comment text
        sed -i '' "s|# fpm-based packager for Template|# fpm-based packager for ${NEW_APP_NAME}|g" "$file"
        # Replace package output file names: krankybeartemplate_${VERSION}...
        sed -i '' "s|krankybeartemplate_\${VERSION}|${SHORT_BINARY_NAME}_\${VERSION}|g" "$file"
        # Replace fpm file mappings: "$SRC_BIN=/opt/local/bin/krankybeartemplate"
        sed -i '' "s|/opt/local/bin/krankybeartemplate|/opt/local/bin/${SHORT_BINARY_NAME}|g" "$file"
        # Replace cp commands: cp -X "$SRC_BIN" "$STAGING_DIR/krankybeartemplate"
        sed -i '' "s|\"\$STAGING_DIR/krankybeartemplate\"|\"\$STAGING_DIR/${SHORT_BINARY_NAME}\"|g" "$file"
        sed -i '' "s|\$STAGING_DIR/krankybeartemplate|\$STAGING_DIR/${SHORT_BINARY_NAME}|g" "$file"
    fi
    
    # Replace "krankybeartemplate" (lowercase package name) with short binary name
    # Do this AFTER package.sh specific replacements
    if grep -qE "krankybeartemplate" "$file.bak" 2>/dev/null
    then
        sed -i '' "s/krankybeartemplate/${SHORT_BINARY_NAME}/g" "$file"
    fi
    
    # Replace icon file references - extract base name from ICON_FILE
    ICON_BASE=$(basename "$ICON_FILE" .png)
    ICON_BASE=$(basename "$ICON_BASE" .ico)
    ICON_BASE=$(basename "$ICON_BASE" .icns)
    
    # Replace KrankyBear* icon references with new icon
    # Handle full paths like Resources/Images/KrankyBearBeret.png
    sed -i '' "s|Resources/Images/KrankyBear[^[:space:]]*\.png|Resources/Images/${ICON_FILE}|g" "$file"
    sed -i '' "s|Resources/Images/KrankyBear[^[:space:]]*\.ico|Resources/Images/${ICON_BASE}.ico|g" "$file"
    sed -i '' "s|Resources/Images/KrankyBear[^[:space:]]*\.icns|Resources/Images/${ICON_BASE}.icns|g" "$file"
    
    # Handle relative paths like ./KrankyBearBeret.png or just KrankyBearBeret.png
    sed -i '' "s|\./KrankyBear[^[:space:]]*\.png|./${ICON_FILE}|g" "$file"
    sed -i '' "s|KrankyBear[^[:space:]]*\.png|${ICON_FILE}|g" "$file"
    sed -i '' "s|KrankyBear[^[:space:]]*\.ico|${ICON_BASE}.ico|g" "$file"
    sed -i '' "s|KrankyBear[^[:space:]]*\.icns|${ICON_BASE}.icns|g" "$file"
    
    # Check if file was actually modified
    if ! cmp -s "$file" "$file.bak"
    then
        CHANGED_FILES=$((CHANGED_FILES + 1))
        TOTAL_REPLACEMENTS=$((TOTAL_REPLACEMENTS + REPLACEMENTS))
        echo "  ✓ Modified: $file ($REPLACEMENTS replacements)"
        # Remove backup if successful
        rm "$file.bak"
    else
        # No changes, remove backup
        rm "$file.bak"
    fi
    
done < <(find_text_files)

# Process .gitignore file
echo ""
echo "Processing .gitignore..."
if [ -f ".gitignore" ]
then
    cp ".gitignore" ".gitignore.bak"
    REPLACEMENTS=0
    
    COUNT=$(grep -o "KrankyBearTemplate" ".gitignore.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    COUNT=$(grep -oE "template-[^[:space:]]*" ".gitignore.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    if [[ $REPLACEMENTS -gt 0 ]]
    then
        # Replace KrankyBearTemplate with new identifier
        sed -i '' "s/KrankyBearTemplate/$IDENTIFIER_NAME/g" ".gitignore"
        
        # Replace template-* patterns with short binary name
        sed -i '' "s/template-macos/${SHORT_BINARY_NAME}-macos/g" ".gitignore"
        sed -i '' "s/template-mac/${SHORT_BINARY_NAME}-mac/g" ".gitignore"
        sed -i '' "s/template-linux/${SHORT_BINARY_NAME}-linux/g" ".gitignore"
        sed -i '' "s/template-windows/${SHORT_BINARY_NAME}-windows/g" ".gitignore"
        
        if ! cmp -s ".gitignore" ".gitignore.bak"
        then
            CHANGED_FILES=$((CHANGED_FILES + 1))
            TOTAL_REPLACEMENTS=$((TOTAL_REPLACEMENTS + REPLACEMENTS))
            echo "  ✓ Modified: .gitignore ($REPLACEMENTS replacements)"
        fi
    fi
    rm -f ".gitignore.bak"
fi

# Process FyneApp.toml file
echo ""
echo "Processing FyneApp.toml..."
if [ -f "FyneApp.toml" ]
then
    cp "FyneApp.toml" "FyneApp.toml.bak"
    REPLACEMENTS=0
    
    # Update ID (bundle identifier) - replace KrankyBear* with new identifier
    if grep -qE 'ID\s*=\s*".*KrankyBear' "FyneApp.toml.bak" 2>/dev/null
    then
        # Replace KrankyBearNotify or KrankyBearTemplate in the ID field
        sed -i '' "s/KrankyBearNotify/${IDENTIFIER_NAME}/g" "FyneApp.toml"
        sed -i '' "s/KrankyBearTemplate/${IDENTIFIER_NAME}/g" "FyneApp.toml"
        REPLACEMENTS=$((REPLACEMENTS + 1))
    fi
    
    # Update Name field - replace the entire name value
    if grep -qE 'Name\s*=' "FyneApp.toml.bak" 2>/dev/null
    then
        sed -i '' "s/\(Name = \"\)[^\"]*\(.*\)/\1${NEW_APP_NAME}\2/" "FyneApp.toml"
        REPLACEMENTS=$((REPLACEMENTS + 1))
    fi
    
    # Update Icon field
    if grep -qE 'Icon\s*=' "FyneApp.toml.bak" 2>/dev/null
    then
        sed -i '' "s/\(Icon = \"\)[^\"]*\(.*\)/\1${ICON_FILE}\2/" "FyneApp.toml"
        REPLACEMENTS=$((REPLACEMENTS + 1))
    fi
    
    if ! cmp -s "FyneApp.toml" "FyneApp.toml.bak"
    then
        CHANGED_FILES=$((CHANGED_FILES + 1))
        TOTAL_REPLACEMENTS=$((TOTAL_REPLACEMENTS + REPLACEMENTS))
        echo "  ✓ Modified: FyneApp.toml ($REPLACEMENTS replacements)"
    fi
    rm -f "FyneApp.toml.bak"
fi

# Process winres directory files
echo ""
echo "Processing winres/* files..."
if [ -d "winres" ]
then
    for file in winres/*
    do
        if [[ -f "$file" ]] && file "$file" | grep -qE "(text|ASCII|UTF-8|JSON)"
        then
            cp "$file" "$file.bak"
            REPLACEMENTS=0
            
            COUNT=$(grep -o "KrankyBearTemplate" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
            REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
            
            # Count icon file references that need updating
            COUNT=$(grep -oE "KrankyBear[^\"\s]*\.(png|ico)" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
            REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
            
            if [[ $REPLACEMENTS -gt 0 ]]
            then
                # Replace KrankyBearTemplate identifier
                sed -i '' "s/KrankyBearTemplate/$IDENTIFIER_NAME/g" "$file"
                
                # Replace icon file references - extract base name from ICON_FILE
                ICON_BASE=$(basename "$ICON_FILE" .png)
                ICON_BASE=$(basename "$ICON_BASE" .ico)
                ICON_BASE=$(basename "$ICON_BASE" .icns)
                
                # Replace KrankyBear* icon references with new icon
                # Handle both .png and .ico extensions, and 64 suffix variants
                # IMPORTANT: Replace 64.png variants FIRST to avoid partial matches
                # Replace patterns like KrankyBearTrapperRedPlaid64.png -> MyIcon64.png
                sed -i '' "s|KrankyBear[^\"\s/]*64\.png|${ICON_BASE}64.png|g" "$file"
                # Replace patterns like KrankyBearTrapperRedPlaid.png -> MyIcon.png
                sed -i '' "s|KrankyBear[^\"\s/]*\.png|${ICON_BASE}.png|g" "$file"
                # Replace .ico files
                sed -i '' "s|KrankyBear[^\"\s/]*\.ico|${ICON_BASE}.ico|g" "$file"
                
                if ! cmp -s "$file" "$file.bak"
                then
                    CHANGED_FILES=$((CHANGED_FILES + 1))
                    TOTAL_REPLACEMENTS=$((TOTAL_REPLACEMENTS + REPLACEMENTS))
                    echo "  ✓ Modified: $file ($REPLACEMENTS replacements)"
                fi
            fi
            rm -f "$file.bak"
        fi
    done
fi

# Generate a new Windows Application ID (GUID)
generate_guid() {
    if command -v uuidgen &> /dev/null
    then
        # Use uuidgen if available (macOS/Linux)
        uuidgen | tr '[:lower:]' '[:upper:]'
    elif command -v python3 &> /dev/null
    then
        # Use Python's uuid module as fallback
        python3 -c "import uuid; print(str(uuid.uuid4()).upper())"
    else
        # Fallback: generate using /dev/urandom (basic GUID format)
        printf "%04X%04X-%04X-%04X-%04X-%04X%04X%04X\n" \
            $((RANDOM % 65536)) $((RANDOM % 65536)) \
            $((RANDOM % 65536)) \
            $((RANDOM % 4096 | 16384)) \
            $((RANDOM % 16384 | 32768)) \
            $((RANDOM % 65536)) $((RANDOM % 65536)) $((RANDOM % 65536))
    fi
}

# Process Inno directory files and rename .iss file
echo ""
echo "Processing Inno/* files..."
if [ -d "Inno" ]
then
    # Process .iss file first (if it exists) to handle renaming
    ISS_FILE="Inno/KrankyBearTemplate.iss"
    if [[ -f "$ISS_FILE" ]]
    then
        echo "  Processing $ISS_FILE..."
        cp "$ISS_FILE" "$ISS_FILE.bak"
        REPLACEMENTS=0
        
        # Count various replacements needed
        COUNT=$(grep -o "KrankyBearTemplate" "$ISS_FILE.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
        REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
        
        COUNT=$(grep -o "template-windows" "$ISS_FILE.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
        REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
        
        COUNT=$(grep -oE "KrankyBear[^\"\s]*\.(ico|png)" "$ISS_FILE.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
        REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
        
        # Generate new Application ID (GUID)
        NEW_APP_ID=$(generate_guid)
        echo "  Generated new Application ID: $NEW_APP_ID"
        
        # Always make replacements if file contains the patterns, even if count is 0
        # Replace MyAppName
        sed -i '' "s/#define MyAppName \"KrankyBearTemplate\"/#define MyAppName \"${IDENTIFIER_NAME}\"/g" "$ISS_FILE"
        
        # Replace MyAppExeName (template-windows.exe -> short-binary-name-windows.exe)
        sed -i '' "s/template-windows\.exe/${SHORT_BINARY_NAME}-windows.exe/g" "$ISS_FILE"
        sed -i '' "s/#define MyAppExeName \"template-windows.exe\"/#define MyAppExeName \"${SHORT_BINARY_NAME}-windows.exe\"/g" "$ISS_FILE"
        
        # Replace MyAppURL if it contains KrankyBearTemplate
        sed -i '' "s|github.com/amarillier/KrankyBearTemplate|github.com/amarillier/${IDENTIFIER_NAME}|g" "$ISS_FILE"
        
        # Replace OutputBaseFilename
        sed -i '' "s/OutputBaseFilename=KrankyBearTemplateSetup/OutputBaseFilename=${IDENTIFIER_NAME}Setup/g" "$ISS_FILE"
        
        # Replace SetupIconFile - extract base name from ICON_FILE
        ICON_BASE=$(basename "$ICON_FILE" .png)
        ICON_BASE=$(basename "$ICON_BASE" .ico)
        ICON_BASE=$(basename "$ICON_BASE" .icns)
        
        # Replace icon file references
        sed -i '' "s|SetupIconFile=..\\\\Resources\\\\Images\\\\KrankyBear[^\"\s]*\.ico|SetupIconFile=..\\\\Resources\\\\Images\\\\${ICON_BASE}.ico|g" "$ISS_FILE"
        
        # Replace Application ID (GUID) - find the AppId line and replace the GUID
        # Pattern: AppId={{4578B785-DB27-44FF-B3F9-2713B327BB90} (note: missing closing brace in original)
        # Use a simpler pattern that matches any GUID format
        sed -i '' "s/\(AppId={{\)[0-9A-F-]\{36\}\}/\1${NEW_APP_ID}}/g" "$ISS_FILE"
        # Also handle case without closing brace at end of line
        sed -i '' "s/\(AppId={{\)[0-9A-F-]\{36\}$/\1${NEW_APP_ID}}/g" "$ISS_FILE"
        
        # Replace KrankyBearTemplate in other places
        sed -i '' "s/KrankyBearTemplate/$IDENTIFIER_NAME/g" "$ISS_FILE"
        
        if ! cmp -s "$ISS_FILE" "$ISS_FILE.bak"
        then
            CHANGED_FILES=$((CHANGED_FILES + 1))
            TOTAL_REPLACEMENTS=$((TOTAL_REPLACEMENTS + REPLACEMENTS))
            echo "  ✓ Modified: $ISS_FILE ($REPLACEMENTS replacements)"
        else
            echo "  ✓ Modified: $ISS_FILE (content updated)"
        fi
        rm -f "$ISS_FILE.bak"
        
        # Rename .iss file
        NEW_ISS_FILE="Inno/${IDENTIFIER_NAME}.iss"
        if [[ "$ISS_FILE" != "$NEW_ISS_FILE" ]]
        then
            mv "$ISS_FILE" "$NEW_ISS_FILE"
            echo "  ✓ Renamed: $ISS_FILE -> $NEW_ISS_FILE"
            
            # Update references to the old .iss filename in other files
            echo "  Updating references to .iss file..."
            find . -type f -not -path "./.git/*" -not -path "./vendor/*" -not -path "./bin/*" -not -path "./installers/*" -not -path "./KrankyBearTemplate.app/*" -not -path "./${IDENTIFIER_NAME}.app/*" -not -name "rename-app.sh" 2>/dev/null | while IFS= read -r ref_file
            do
                if [[ -f "$ref_file" ]] && grep -q "KrankyBearTemplate.iss" "$ref_file" 2>/dev/null
                then
                    sed -i '' "s/KrankyBearTemplate\.iss/${IDENTIFIER_NAME}.iss/g" "$ref_file"
                    echo "    ✓ Updated reference in: $ref_file"
                fi
            done
        fi
    fi
    
    # Process other files in Inno directory
    for file in Inno/*
    do
        # Skip the .iss file we already processed
        if [[ "$file" == "Inno/KrankyBearTemplate.iss" ]] || [[ "$file" == "Inno/${IDENTIFIER_NAME}.iss" ]]
        then
            continue
        fi
        
        if [[ -f "$file" ]] && file "$file" | grep -qE "(text|ASCII|UTF-8)"
        then
            cp "$file" "$file.bak"
            REPLACEMENTS=0
            
            COUNT=$(grep -o "KrankyBearTemplate" "$file.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
            REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
            
            if [[ $REPLACEMENTS -gt 0 ]]
            then
                sed -i '' "s/KrankyBearTemplate/$IDENTIFIER_NAME/g" "$file"
                if ! cmp -s "$file" "$file.bak"
                then
                    CHANGED_FILES=$((CHANGED_FILES + 1))
                    TOTAL_REPLACEMENTS=$((TOTAL_REPLACEMENTS + REPLACEMENTS))
                    echo "  ✓ Modified: $file ($REPLACEMENTS replacements)"
                fi
            fi
            rm -f "$file.bak"
        fi
    done
fi

# Process Info-plist.txt in main directory
echo ""
echo "Processing Info-plist.txt..."
if [ -f "Info-plist.txt" ]
then
    cp "Info-plist.txt" "Info-plist.txt.bak"
    REPLACEMENTS=0
    
    COUNT=$(grep -o "KrankyBearTemplate\|KrankyBearTailer" "Info-plist.txt.bak" 2>/dev/null | wc -l | tr -d '[:space:]' || echo 0)
    REPLACEMENTS=$((REPLACEMENTS + ${COUNT:-0}))
    
    if [[ $REPLACEMENTS -gt 0 ]]
    then
        # Replace CFBundleName
        sed -i '' "s/<string>KrankyBearTemplate<\/string>/<string>${IDENTIFIER_NAME}<\/string>/g" "Info-plist.txt"
        sed -i '' "s/<string>KrankyBearTailer<\/string>/<string>${IDENTIFIER_NAME}<\/string>/g" "Info-plist.txt"
        
        # Replace CFBundleExecutable
        sed -i '' "/<key>CFBundleExecutable<\/key>/,/<\/string>/{
            s/<string>KrankyBearTemplate<\/string>/<string>${IDENTIFIER_NAME}<\/string>/
            s/<string>KrankyBearTailer<\/string>/<string>${IDENTIFIER_NAME}<\/string>/
        }" "Info-plist.txt"
        
        # Replace CFBundleIdentifier
        sed -i '' "s/com\.github\.amarillier\.KrankyBearTemplate/com.github.amarillier.${IDENTIFIER_NAME}/g" "Info-plist.txt"
        sed -i '' "s/com\.github\.amarillier\.KrankyBearTailer/com.github.amarillier.${IDENTIFIER_NAME}/g" "Info-plist.txt"
        sed -i '' "s/<string>com\.github\.amarillier\.KrankyBear[^<]*<\/string>/<string>com.github.amarillier.${IDENTIFIER_NAME}<\/string>/g" "Info-plist.txt"
        
        # Replace KrankyBearTemplate/KrankyBearTailer in other places
        sed -i '' "s/KrankyBearTemplate/$IDENTIFIER_NAME/g" "Info-plist.txt"
        sed -i '' "s/KrankyBearTailer/$IDENTIFIER_NAME/g" "Info-plist.txt"
        
        if ! cmp -s "Info-plist.txt" "Info-plist.txt.bak"
        then
            CHANGED_FILES=$((CHANGED_FILES + 1))
            TOTAL_REPLACEMENTS=$((TOTAL_REPLACEMENTS + REPLACEMENTS))
            echo "  ✓ Modified: Info-plist.txt ($REPLACEMENTS replacements)"
        fi
    fi
    rm -f "Info-plist.txt.bak"
fi

# Rename KrankyBearTemplate.app directory and update Info-plist.txt
echo ""
echo "Renaming application bundle directory..."
if [ -d "KrankyBearTemplate.app" ]
then
    NEW_APP_DIR="${IDENTIFIER_NAME}.app"
    if [[ "KrankyBearTemplate.app" != "$NEW_APP_DIR" ]]
    then
        mv "KrankyBearTemplate.app" "$NEW_APP_DIR"
        echo "  ✓ Renamed: KrankyBearTemplate.app -> $NEW_APP_DIR"
        
        # Update Info-plist.txt in the .app bundle
        INFO_PLIST="${NEW_APP_DIR}/Contents/Info-plist.txt"
        if [[ -f "$INFO_PLIST" ]]
        then
            echo "  Updating Info-plist.txt..."
            cp "$INFO_PLIST" "$INFO_PLIST.bak"
            
            # Replace CFBundleName (the string value after <key>CFBundleName</key>)
            sed -i '' "s/<string>KrankyBearTemplate<\/string>/<string>${IDENTIFIER_NAME}<\/string>/g" "$INFO_PLIST"
            
            # Replace CFBundleExecutable (the string value after <key>CFBundleExecutable</key>)
            # Use a simpler approach: replace the string value that appears after CFBundleExecutable key
            sed -i '' "/<key>CFBundleExecutable<\/key>/,/<\/string>/{
                s/<string>KrankyBearTemplate<\/string>/<string>${IDENTIFIER_NAME}<\/string>/
            }" "$INFO_PLIST"
            
            # Replace CFBundleIdentifier
            sed -i '' "s/com\.github\.amarillier\.KrankyBearTemplate/com.github.amarillier.${IDENTIFIER_NAME}/g" "$INFO_PLIST"
            sed -i '' "s/<string>com\.github\.amarillier\.KrankyBear[^<]*<\/string>/<string>com.github.amarillier.${IDENTIFIER_NAME}<\/string>/g" "$INFO_PLIST"
            
            # Replace KrankyBearTemplate in other places
            sed -i '' "s/KrankyBearTemplate/$IDENTIFIER_NAME/g" "$INFO_PLIST"
            
            if ! cmp -s "$INFO_PLIST" "$INFO_PLIST.bak"
            then
                CHANGED_FILES=$((CHANGED_FILES + 1))
                echo "  ✓ Modified: $INFO_PLIST"
            fi
            rm -f "$INFO_PLIST.bak"
        fi
        
        # Also update any references to the old .app path in files
        echo "  Updating references to .app directory in files..."
        find . -type f -not -path "./.git/*" -not -path "./vendor/*" -not -path "./bin/*" -not -path "./installers/*" -not -path "./${NEW_APP_DIR}/*" -not -name "rename-app.sh" 2>/dev/null | grep -v "^\./\." | while IFS= read -r file
        do
            if [[ -f "$file" ]] && grep -q "KrankyBearTemplate.app" "$file" 2>/dev/null
            then
                sed -i '' "s/KrankyBearTemplate\.app/${IDENTIFIER_NAME}.app/g" "$file"
                echo "    ✓ Updated: $file"
            fi
        done
    else
        # Even if directory name didn't change, still update Info-plist.txt
        INFO_PLIST="KrankyBearTemplate.app/Contents/Info-plist.txt"
        if [[ -f "$INFO_PLIST" ]]
        then
            echo "  Updating Info-plist.txt..."
            cp "$INFO_PLIST" "$INFO_PLIST.bak"
            
            # Replace CFBundleName (the string value after <key>CFBundleName</key>)
            sed -i '' "s/<string>KrankyBearTemplate<\/string>/<string>${IDENTIFIER_NAME}<\/string>/g" "$INFO_PLIST"
            
            # Replace CFBundleExecutable (the string value after <key>CFBundleExecutable</key>)
            # Use a simpler approach: replace the string value that appears after CFBundleExecutable key
            sed -i '' "/<key>CFBundleExecutable<\/key>/,/<\/string>/{
                s/<string>KrankyBearTemplate<\/string>/<string>${IDENTIFIER_NAME}<\/string>/
            }" "$INFO_PLIST"
            
            # Replace CFBundleIdentifier
            sed -i '' "s/com\.github\.amarillier\.KrankyBearTemplate/com.github.amarillier.${IDENTIFIER_NAME}/g" "$INFO_PLIST"
            sed -i '' "s/<string>com\.github\.amarillier\.KrankyBear[^<]*<\/string>/<string>com.github.amarillier.${IDENTIFIER_NAME}<\/string>/g" "$INFO_PLIST"
            
            # Replace KrankyBearTemplate in other places
            sed -i '' "s/KrankyBearTemplate/$IDENTIFIER_NAME/g" "$INFO_PLIST"
            
            if ! cmp -s "$INFO_PLIST" "$INFO_PLIST.bak"
            then
                CHANGED_FILES=$((CHANGED_FILES + 1))
                echo "  ✓ Modified: $INFO_PLIST"
            fi
            rm -f "$INFO_PLIST.bak"
        fi
    fi
fi

echo ""
echo "=========================================="
echo "Rename Complete!"
echo "=========================================="
echo "Files modified: $CHANGED_FILES"
echo "Total replacements: $TOTAL_REPLACEMENTS"
echo ""
echo "Next steps:"
echo "  1. Review the changes with: git diff"
echo "  2. Verify icon references are correct (icon: $ICON_FILE)"
echo "  3. Test compilation with: ./compile-mac.sh"
echo "  4. Check that FyneApp.toml has been updated correctly"
echo ""

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
