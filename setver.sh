#! /bin/sh

if [ $# -ge 1 ]
then
    ver=$1
else
    echo "Enter a version number"
    cur=$(cat main.go | grep -i "Version =")
    echo "    current: $cur"
    read ver
    if [ -z "$ver" ]
    then
        echo "Enter a version!"
        echo "No version change detected, continuing to allow compile to continue"
        exit
    else
        echo "Version: $ver"
        # exit
    fi
fi

echo "version: $ver"
echo "main.go"
sed -i '' "s/Version = \".*\"/Version = \"$ver\"/" main.go

echo "FyneApp.toml"
sed -i '' "s/Version = \".*\"/Version = \"$ver\"/" FyneApp.toml

echo "Inno Setup Inno/KrankyBearFileMover.iss"
sed -i '' "s/MyAppVersion \".*\"/MyAppVersion \"$ver\"/" ./Inno/KrankyBearFileMover.iss

echo "Inno Setup winres/winres.json"
sed -i '' "s/file_version\":.*/file_version\": \"$ver\",/" ./winres/winres.json
sed -i '' "s/product_version\":.*/product_version\": \"$ver\"/" ./winres/winres.json
sed -i '' "s/FileVersion\":.*/FileVersion\": \"$ver\",/" ./winres/winres.json
sed -i '' "s/ProductVersion\":.*/ProductVersion\": \"$ver\",/" ./winres/winres.json

echo "No direct Info.plist updates - updating Info-plist.txt which can be renamed if wanted"
sed -i '' "s/<string>v .*<\/string>/<string>v $ver<\/string>/" ./Info-plist.txt

#echo "mkAllZip.sh"
#sed -i '' "s/version=\".*\"/version=\"$ver\"/" mkAllZip.sh
#sed -i '' "s/\/.*{flag=1}\//\/$ver\/{flag=1}\//" mkAllZip.sh

echo "Update LICENSE and ReleaseNotes.txt"
cp LICENSE Resources
cp ReleaseNotes.txt ./Resources
cp LICENSE KrankyBearTailer.app/Contents/Resources
cp ReleaseNotes.txt ./KrankyBearTailer.app/Contents/Resources

echo "Update package.sh"
sed -i '' "s/VERSION:-.*}/VERSION:-$ver}/" ./package.sh

# "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
