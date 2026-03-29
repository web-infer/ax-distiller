package chrome

import (
	"ax-distiller/internal/chrome/cdp"
	"ax-distiller/internal/chrome/fastclient"
	"context"
	"os"

	"github.com/go-rod/rod"
	rodcdp "github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func NewBrowser(chromeBin string) (browser *rod.Browser, err error) {
	dataTemp := "/tmp/ax-distiller/chrome-data"
	err = os.RemoveAll(dataTemp)
	if err != nil {
		return
	}
	err = os.MkdirAll(dataTemp, 0700)
	if err != nil {
		return
	}

	launch := launcher.New().Bin(chromeBin).
		UserDataDir(dataTemp).
		Headless(false).
		Set("display", os.Getenv("DISPLAY")).
		Set("disable-blink-features", "AutomationControlled")

	controlURL := launch.MustLaunch()
	browser = rod.New()
	client := fastclient.New()
	ws := &rodcdp.WebSocket{}
	err = ws.Connect(browser.GetContext(), controlURL, nil)
	if err != nil {
		panic(err)
	}

	client.Start(ws)
	browser.Client(client)
	browser.MustConnect()
	return
}

// BlockGraphics applies a routing configuration to the given page which blocks
// the loading of images and videos.
func BlockGraphics(page *rod.Page) {
	router := page.HijackRequests()

	for _, ext := range []string{
		"*.png",
		"*.jpg",
		"*.jpeg",
		"*.bmp",
		"*.gif",
		"*.webp",
		"*.heic",
		"*.heif",
		"*.tiff",
		"*.tif",
		"*.mp4",
		"*.avi",
		"*.mov",
		"*.mkv",
		"*.webm",
		"*.ts",
		"*.ogv",
	} {
		router.MustAdd(ext, blockImageOrMedia)
	}

	// since we are only hijacking a specific page, even using the "*" won't affect much of the performance
	go router.Run()
}

func blockImageOrMedia(ctx *rod.Hijack) {
	switch ctx.Request.Type() {
	case proto.NetworkResourceTypeImage, proto.NetworkResourceTypeMedia:
		ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
		return
	}
	ctx.ContinueRequest(&proto.FetchContinueRequest{})
}

func DisableUnusedCDP(page *rod.Page) (err error) {
	err = cdp.CommandUnary(context.TODO(), page, proto.NetworkDisable{})
	if err != nil {
		return
	}
	err = cdp.CommandUnary(context.TODO(), page, proto.LogDisable{})
	if err != nil {
		return
	}
	err = cdp.CommandUnary(context.TODO(), page, proto.CSSDisable{})
	return
}
