/*
 * SPDX-License-Identifier: GPL-3.0
 * Vencord Installer, a cross platform gui/cli app for installing Vencord
 * Copyright (c) 2023 Vendicated and Vencord contributors
 */

package main

import (
	"errors"
	"github.com/ProtonMail/go-appdir"
	"os"
	"os/exec"
	path "path/filepath"
	"strings"
)

var BaseDir string
var FilesDir string
var FilesDirErr error
var Patcher string

func init() {
	if dir := os.Getenv("VENCORD_USER_DATA_DIR"); dir != "" {
		Log.Debug("Using VENCORD_USER_DATA_DIR")
		BaseDir = dir
	} else if dir = os.Getenv("DISCORD_USER_DATA_DIR"); dir != "" {
		Log.Debug("Using DISCORD_USER_DATA_DIR/../VencordData")
		BaseDir = path.Join(dir, "..", "VencordData")
	} else {
		Log.Debug("Using UserConfig")
		BaseDir = appdir.New("Vencord").UserConfig()
	}
	FilesDir = path.Join(BaseDir, "dist")
	if !ExistsFile(FilesDir) {
		FilesDirErr = os.MkdirAll(FilesDir, 0755)
		if FilesDirErr != nil {
			Log.Error("Failed to create", FilesDir, FilesDirErr)
		} else {
			FilesDirErr = FixOwnership(BaseDir)
		}
	}
	Patcher = path.Join(FilesDir, "patcher.js")
}

type DiscordInstall struct {
	path             string // the base path
	branch           string // canary / stable / ...
	appPath          string // List of app folder to patch
	isPatched        bool
	isFlatpak        bool
	isSystemElectron bool // Needs special care https://aur.archlinux.org/packages/discord_arch_electron
	isOpenAsar       *bool
}

//region Patch

func patchAppAsar(dir string, isSystemElectron bool) (err error) {
	appAsar := path.Join(dir, "app.asar")
	_appAsar := path.Join(dir, "_app.asar")

	var renamesDone [][]string
	defer func() {
		if err != nil && len(renamesDone) > 0 {
			Log.Error(T("Failed to patch. Undoing partial patch", "Не удалось пропатчить. Откатываю частичный патч"))
			for _, rename := range renamesDone {
				if innerErr := os.Rename(rename[1], rename[0]); innerErr != nil {
					Log.Error(T("Failed to undo partial patch. This install is probably bricked.", "Не удалось откатить. Установка, вероятно, сломана."), innerErr)
				} else {
					Log.Info(T("Successfully undid all changes", "Изменения успешно откачены"))
				}
			}
		}
	}()

	Log.Debug("Renaming", appAsar, "to", _appAsar)
	if err := os.Rename(appAsar, _appAsar); err != nil {
		err = CheckIfErrIsCauseItsBusyRn(err)
		Log.Error(err.Error())
		return err
	}
	renamesDone = append(renamesDone, []string{appAsar, _appAsar})

	if isSystemElectron {
		from, to := appAsar+".unpacked", _appAsar+".unpacked"
		Log.Debug("Renaming", from, "to", to)
		err := os.Rename(from, to)
		if err != nil {
			return err
		}
		renamesDone = append(renamesDone, []string{from, to})
	}

	Log.Debug("Writing custom app.asar to", appAsar)
	if err := WriteAppAsar(appAsar, Patcher); err != nil {
		return err
	}

	return nil
}

func (di *DiscordInstall) patch() error {
	Log.Info(T("Patching ", "Патчу ") + di.path + "...")
	if LatestHash != InstalledHash {
		if err := InstallLatestBuilds(); err != nil {
			return nil // already shown dialog so don't return same error again
		}
	}

	PreparePatch(di)

	if di.isPatched {
		Log.Info(di.path, T("is already patched. Unpatching first...", "уже пропатчен. Сначала откатываю..."))
		if err := di.unpatch(); err != nil {
			if errors.Is(err, os.ErrPermission) {
				return err
			}
			return errors.New(T("patch: Failed to unpatch already patched install '", "патч: не удалось откатить уже пропатченную установку '") + di.path + "':\n" + err.Error())
		}
	}

	if di.isSystemElectron {
		if err := patchAppAsar(di.path, true); err != nil {
			return err
		}
	} else {
		if err := patchAppAsar(path.Join(di.appPath, ".."), false); err != nil {
			return err
		}
	}

	Log.Info(T("Successfully patched", "Успешно пропатчено"), di.path)
	di.isPatched = true

	if di.isFlatpak {
		pathElements := strings.Split(di.path, "/")
		var name string
		for _, e := range pathElements {
			if strings.HasPrefix(e, "com.discordapp") {
				name = e
				break
			}
		}

		Log.Debug("This is a flatpak. Trying to grant the Flatpak access to", FilesDir+"...")

		isSystemFlatpak := strings.HasPrefix(di.path, "/var")
		var args []string
		if !isSystemFlatpak {
			args = append(args, "--user")
		}
		args = append(args, "override", name, "--filesystem="+FilesDir)
		fullCmd := "flatpak " + strings.Join(args, " ")

		Log.Debug("Running", fullCmd)

		var err error
		if !isSystemFlatpak && os.Getuid() == 0 {
			// We are operating on a user flatpak but are root
			actualUser := os.Getenv("SUDO_USER")
			Log.Debug("This is a user install but we are root. Using su to run as", actualUser)
			cmd := exec.Command("su", "-", actualUser, "-c", "sh", "-c", fullCmd)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err = cmd.Run()
		} else {
			cmd := exec.Command("flatpak", args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err = cmd.Run()
		}
		if err != nil {
			return errors.New(T("Failed to grant Discord Flatpak access to ", "Не удалось дать Flatpak Discord доступ к ") + FilesDir + ": " + err.Error())
		}
	}
	return nil
}

//endregion

// region Unpatch

func unpatchAppAsar(dir string, isSystemElectron bool) (errOut error) {
	appAsar := path.Join(dir, "app.asar")
	appAsarTmp := path.Join(dir, "app.asar.tmp")
	_appAsar := path.Join(dir, "_app.asar")

	var renamesDone [][]string
	defer func() {
		if errOut != nil && len(renamesDone) > 0 {
			Log.Error(T("Failed to unpatch. Undoing partial unpatch", "Не удалось откатить. Откатываю частичный откат"))
			for _, rename := range renamesDone {
				if innerErr := os.Rename(rename[1], rename[0]); innerErr != nil {
					Log.Error(T("Failed to undo partial unpatch. This install is probably bricked.", "Не удалось откатить. Установка, вероятно, сломана."), innerErr)
				} else {
					Log.Info(T("Successfully undid all changes", "Изменения успешно откачены"))
				}
			}
		} else if errOut == nil {
			if innerErr := os.RemoveAll(appAsarTmp); innerErr != nil {
				Log.Warn(T("Failed to delete temporary app.asar (patch folder) backup. This is whatever but you might want to delete it manually.", "Не удалось удалить временный бэкап app.asar. Не страшно, но можете удалить вручную."), innerErr)
			}
		}
	}()

	Log.Debug("Deleting", appAsar)
	if err := os.Rename(appAsar, appAsarTmp); err != nil {
		err = CheckIfErrIsCauseItsBusyRn(err)
		Log.Error(err.Error())
		errOut = err
	} else {
		renamesDone = append(renamesDone, []string{appAsar, appAsarTmp})
	}

	Log.Debug("Renaming", _appAsar, "to", appAsar)
	if err := os.Rename(_appAsar, appAsar); err != nil {
		err = CheckIfErrIsCauseItsBusyRn(err)
		Log.Error(err.Error())
		errOut = err
	} else {
		renamesDone = append(renamesDone, []string{_appAsar, appAsar})
	}

	if isSystemElectron {
		Log.Debug("Renaming", _appAsar+".unpacked", "to", appAsar+".unpacked")
		if err := os.Rename(_appAsar+".unpacked", appAsar+".unpacked"); err != nil {
			Log.Error(err.Error())
			errOut = err
		}
	}
	return
}

func (di *DiscordInstall) unpatch() error {
	Log.Info(T("Unpatching ", "Откатываю ") + di.path + "...")

	PreparePatch(di)

	if di.isSystemElectron {
		if err := unpatchAppAsar(di.path, true); err != nil {
			return err
		}
	} else {
		if err := unpatchAppAsar(path.Join(di.appPath, ".."), false); err != nil {
			return err
		}
	}

	Log.Info(T("Successfully unpatched", "Успешно откачено"), di.path)
	di.isPatched = false
	return nil
}

//endregion
