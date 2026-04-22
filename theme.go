//go:generate fyne bundle -o bundled.go -a assets/images/KrankyBearBeret.png
//go:generate fyne bundle -o bundled.go -a assets/images/KrankyBearCowboyBrown.png
//go:generate fyne bundle -o bundled.go -a assets/images/KrankyBearChristmasGrinch.png

package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type appTheme struct {
	fyne.Theme
}

func newAppTheme() *appTheme {
	return &appTheme{
		Theme: theme.DefaultTheme(),
	}
}

func (a *appTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameHeadingText {
		return a.Theme.Size(n) * 1.5
	}

	return a.Theme.Size(n)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
