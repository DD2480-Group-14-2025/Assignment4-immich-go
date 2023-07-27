package main

import (
	"archive/zip"
	"context"
	"fmt"
	"immich-go/immich"
	"immich-go/immich/assets"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
)

var stripSpaces = regexp.MustCompile(`\s+`)

func main() {
	app, err := Initialize()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// Create a context with cancel function to gracefully handle Ctrl+C events
	ctx, cancel := context.WithCancel(context.Background())

	// Handle Ctrl+C signal (SIGINT)
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt)

	go func() {
		<-signalChannel
		fmt.Println("\nCtrl+C received. Gracefully shutting down...")
		cancel() // Cancel the context when Ctrl+C is received
	}()

	select {
	case <-ctx.Done():
		err = ctx.Err()
	default:
		err = app.Run(ctx)
	}
	if err != nil {
		app.Logger.Error(err.Error())
		os.Exit(1)
	}
	app.Logger.OK("Done.")
}

func (app *Application) Run(ctx context.Context) error {
	var err error
	app.Immich, err = immich.NewImmichClient(app.EndPoint, app.Key, app.DeviceUUID)
	if err != nil {
		return err
	}

	err = app.Immich.PingServer()
	if err != nil {
		return err
	}
	app.Logger.OK("Server status: OK")

	user, err := app.Immich.ValidateConnection()
	if err != nil {
		return err
	}
	app.Logger.Info("Connected, user: %s", user.Email)
	app.Logger.Info("Get server's assets...")

	app.AssetIndex, err = app.Immich.GetAllAssets(nil)
	if err != nil {
		return err
	}
	app.Logger.OK("%d assets on the server.", app.AssetIndex.Len())

	fsys, err := app.OpenFSs()
	if err != nil {
		return err
	}

	var browser assets.Browser

	switch {
	case app.GooglePhotos:
		app.Logger.Info("Browswing google take out archive...")
		browser, err = app.ReadGoogleTakeOut(ctx, fsys)
	default:
		app.Logger.Info("Browswing folder(s)...")
		browser, err = app.ExploreLocalFolder(ctx, fsys)
	}

	if err != nil {
		return err
	}
	assetChan := browser.Browse(ctx)
assetLoop:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case a, ok := <-assetChan:
			if !ok {
				break assetLoop
			}
			err = app.handleAsset(a)
			if err != nil {
				return err
			}

		}
	}

	if len(app.updateAlbums) > 0 {
		serverAlbums, err := app.Immich.GetAllAlbums()
		if err != nil {
			return fmt.Errorf("can't get the album list from the server: %w", err)
		}
		for album, list := range app.updateAlbums {
			found := false
			for _, sal := range serverAlbums {
				if sal.AlbumName == album {
					found = true
					app.Logger.OK("Update the album %s", album)
					_, err := app.Immich.UpdateAlbum(sal.ID, list)
					if err != nil {
						return fmt.Errorf("can't update the album list from the server: %w", err)
					}
				}
			}
			if found {
				continue
			}
			app.Logger.Info("Create the album %s", album)
			_, err := app.Immich.CreateAlbum(album, list)
			if err != nil {
				return fmt.Errorf("can't create the album list from the server: %w", err)
			}
		}
	}

	if len(app.deleteServerList) > 0 {
		ids := []string{}
		for _, da := range app.deleteServerList {
			ids = append(ids, da.ID)
		}
		err := app.DeleteServerAssets(ids)
		if err != nil {
			return fmt.Errorf("Can't delete server's assets: %w", err)
		}
	}

	if len(app.deleteLocalList) > 0 {
		err = app.DeleteLocalAssets()
	}
	return err
}

func (app *Application) handleAsset(a *assets.LocalAssetFile) error {
	showCount := true
	defer func() {
		a.Close()
		if showCount {
			app.Logger.Progress("%d media scanned", app.mediaCount)
		}
	}()
	app.mediaCount++

	if !app.KeepPartner && a.FromPartner {
		return nil
	}

	if !app.KeepTrashed && a.Trashed {
		return nil
	}

	if len(app.ImportFromAlbum) > 0 && a.Album != app.ImportFromAlbum {
		return nil
	}

	if app.DateRange.IsSet() {
		d, err := a.DateTaken()
		if err != nil {
			app.Logger.Error("Can't get capture date of the file. File %q skiped", a.FileName)
			return nil
		}
		if !app.DateRange.InRange(d) {
			return nil
		}
	}

	advice, _ := app.AssetIndex.ShouldUpload(a)
	switch advice.Advice {
	case immich.NotOnServer:
		app.Logger.Info("%s: %s", a.Title, advice.Message)
		app.UploadAsset(a)
		if app.Delete {
			app.deleteLocalList = append(app.deleteLocalList, a)
		}
	case immich.SmallerOnServer:
		app.Logger.Info("%s: %s", a.Title, advice.Message)
		app.UploadAsset(a)
		app.deleteServerList = append(app.deleteServerList, advice.ServerAsset)
		if app.Delete {
			app.deleteLocalList = append(app.deleteLocalList, a)
		}
	case immich.SameOnServer:
		if !advice.ServerAsset.JustUploaded {
			app.Logger.Info("%s: %s", a.Title, advice.Message)
			if app.Delete {
				app.deleteLocalList = append(app.deleteLocalList, a)
			}
		} else {
			return nil
		}
	}
	showCount = false
	return nil
}

func (app *Application) UploadAsset(a *assets.LocalAssetFile) {
	resp, err := app.Immich.AssetUpload(a)

	if err != nil {
		app.Logger.Error("Can't upload file: %q, %s", a.FileName, err)
		return
	}
	app.AssetIndex.AddLocalAsset(a)
	app.mediaUploaded += 1
	app.Logger.OK("%q uploaded, %d uploaded", a.Title, app.mediaUploaded)

	switch {
	case len(app.ImportIntoAlbum) > 0:
		l := app.updateAlbums[app.ImportIntoAlbum]
		l = append(l, resp.ID)
		app.updateAlbums[app.ImportIntoAlbum] = l
	case len(app.ImportFromAlbum) > 0:
		l := app.updateAlbums[a.Album]
		l = append(l, resp.ID)
		app.updateAlbums[a.Album] = l
	case app.CreateAlbums && len(a.Album) > 0:
		l := app.updateAlbums[a.Album]
		l = append(l, resp.ID)
		app.updateAlbums[a.Album] = l
	}
}

func (a *Application) ReadGoogleTakeOut(ctx context.Context, fsys fs.FS) (assets.Browser, error) {
	a.Delete = false
	return assets.BrowseGooglePhotosAssets(fsys), nil
}

func (a *Application) ExploreLocalFolder(ctx context.Context, fsys fs.FS) (assets.Browser, error) {
	return assets.BrowseLocalAssets(fsys), nil
}

func (a *Application) OpenFSs() (fs.FS, error) {
	fss := []fs.FS{}

	for _, p := range a.Paths {
		s, err := os.Stat(p)
		if err != nil {
			return nil, err
		}

		switch {
		case !s.IsDir() && strings.ToLower(filepath.Ext(s.Name())) == ".zip":
			fsys, err := zip.OpenReader(p)
			if err != nil {
				return nil, err
			}
			fss = append(fss, fsys)
		default:
			fsys := DirRemoveFS(p)
			fss = append(fss, fsys)
		}
	}
	return assets.NewMergedFS(fss), nil
}

func (app *Application) DeleteLocalAssets() error {
	app.Logger.OK("%d local assets to delete.", len(app.deleteLocalList))

	for _, a := range app.deleteLocalList {
		app.Logger.Warning("delete file %q", a.Title)
		err := a.Remove()
		if err != nil {
			return err
		}
	}
	return nil
}

func (app *Application) DeleteServerAssets(ids []string) error {
	app.Logger.Warning("%d server assets to delete.", len(ids))

	_, err := app.Immich.DeleteAsset(ids)
	return err

}
