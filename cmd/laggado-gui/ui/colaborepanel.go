package ui

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// NewColaborePanel builds the support/donate screen (PIX + PayPal + GitHub).
// O contador de Laggers e o seletor de idioma foram movidos para a sidebar.
func NewColaborePanel(state *AppState) fyne.CanvasObject {
	title := canvas.NewText(T("collab.title"), ColorTextPrim)
	title.TextSize = 22
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	sub := canvas.NewText(T("collab.sub"), ColorTextSec)
	sub.TextSize = 13
	sub.Alignment = fyne.TextAlignCenter

	sub2 := canvas.NewText(T("collab.sub2"), ColorTextSec)
	sub2.TextSize = 12
	sub2.Alignment = fyne.TextAlignCenter

	sep := widget.NewSeparator()

	// ── PIX section ───────────────────────────────────────────────
	pixTitle := canvas.NewText("PIX", ColorTextPrim)
	pixTitle.TextSize = 16
	pixTitle.TextStyle = fyne.TextStyle{Bold: true}

	pixQR := makePixQRCode()

	pixKey := canvas.NewText("kaueramone@live.com", ColorAccent)
	pixKey.TextSize = 14
	pixKey.TextStyle = fyne.TextStyle{Bold: true}
	pixKey.Alignment = fyne.TextAlignCenter

	pixHint := canvas.NewText("Chave PIX • qualquer valor ajuda!", ColorTextDim)
	pixHint.TextSize = 11
	pixHint.Alignment = fyne.TextAlignCenter

	pixBg := canvas.NewRectangle(ColorCard)
	pixBg.CornerRadius = 12

	pixBlock := container.NewStack(pixBg, container.NewPadded(container.NewVBox(
		container.NewCenter(pixTitle),
		spacer(8),
		container.NewCenter(pixQR),
		spacer(8),
		container.NewCenter(pixKey),
		container.NewCenter(pixHint),
	)))

	// ── PayPal section ────────────────────────────────────────────
	ppTitle := canvas.NewText("PayPal", ColorTextPrim)
	ppTitle.TextSize = 16
	ppTitle.TextStyle = fyne.TextStyle{Bold: true}

	ppURL, _ := url.Parse("https://paypal.me/kaueramone")
	ppLink := widget.NewHyperlink("paypal.me/kaueramone", ppURL)

	ppHint := canvas.NewText("Clique no link acima para doar pelo PayPal", ColorTextDim)
	ppHint.TextSize = 11
	ppHint.Alignment = fyne.TextAlignCenter

	ppBg := canvas.NewRectangle(ColorCard)
	ppBg.CornerRadius = 12

	ppBlock := container.NewStack(ppBg, container.NewPadded(container.NewVBox(
		container.NewCenter(ppTitle),
		spacer(8),
		container.NewCenter(ppLink),
		spacer(4),
		container.NewCenter(ppHint),
	)))

	// ── GitHub ────────────────────────────────────────────────────
	ghHint := canvas.NewText("Contribua com código, reporte bugs ou dê uma estrela no GitHub 🌟", ColorTextDim)
	ghHint.TextSize = 11
	ghHint.Alignment = fyne.TextAlignCenter

	// ── Layout ────────────────────────────────────────────────────
	twoCol := container.NewGridWithColumns(2,
		container.NewPadded(pixBlock),
		container.NewPadded(ppBlock),
	)

	content := container.NewVBox(
		spacer(20),
		container.NewCenter(title),
		spacer(6),
		container.NewCenter(sub),
		container.NewCenter(sub2),
		spacer(16),
		sep,
		spacer(12),
		twoCol,
		spacer(16),
		container.NewCenter(ghHint),
		spacer(20),
	)

	return container.NewStack(
		canvas.NewRectangle(ColorBG),
		container.NewVScroll(container.NewPadded(content)),
	)
}

// makePixQRCode returns the real PIX static QR Code image for kaueramone@live.com.
func makePixQRCode() fyne.CanvasObject {
	res := fyne.NewStaticResource("pix_qr.png", pixQRBytes)
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(180, 180))
	return img
}
