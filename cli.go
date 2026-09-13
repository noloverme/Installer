//go:build cli

/*
 * SPDX-License-Identifier: GPL-3.0
 * Vencord Installer, a cross platform gui/cli app for installing Vencord
 * Copyright (c) 2023 Vendicated and Vencord contributors
 */

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"vencordinstaller/buildinfo"

	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
)

var discords []any
var interactive = false

func isValidBranch(branch string) bool {
	switch branch {
	case "", "stable", "ptb", "canary", "auto":
		return true
	default:
		return false
	}
}

func die(msg string) {
	Log.Error(msg)
	exitFailure()
}

func main() {
	// Pre-scan --lang before registering flags so that --help text itself
	// is already translated (flag descriptions are fixed at registration).
	for i, a := range os.Args {
		if a == "--lang" && i+1 < len(os.Args) {
			SetLang(os.Args[i+1])
		} else if strings.HasPrefix(a, "--lang=") {
			SetLang(strings.TrimPrefix(a, "--lang="))
		}
	}
	InitGithubDownloader()
	discords = FindDiscords()

	// Used by log.go init func
	flag.Bool("debug", false, T("Enable debug info", "Включить отладку"))

	var helpFlag = flag.Bool("help", false, T("View usage instructions", "Показать справку"))
	var versionFlag = flag.Bool("version", false, T("View the program version", "Показать версию"))
	var updateSelfFlag = flag.Bool("update-self", false, T("Update me to the latest version", "Обновить меня до последней версии"))
	var installFlag = flag.Bool("install", false, T("Install Vencord", "Установить Vencord"))
	var updateFlag = flag.Bool("repair", false, T("Repair Vencord", "Починить Vencord"))
	var uninstallFlag = flag.Bool("uninstall", false, T("Uninstall Vencord", "Удалить Vencord"))
	var installOpenAsarFlag = flag.Bool("install-openasar", false, T("Install OpenAsar", "Установить OpenAsar"))
	var uninstallOpenAsarFlag = flag.Bool("uninstall-openasar", false, T("Uninstall OpenAsar", "Удалить OpenAsar"))
	var locationFlag = flag.String("location", "", T("The location of the Discord install to modify", "Путь к установке Discord"))
	var branchFlag = flag.String("branch", "", T("The branch of Discord to modify [auto|stable|ptb|canary]", "Ветка Discord [auto|stable|ptb|canary]"))
	var langFlag = flag.String("lang", "", T("UI language [en|ru] (default: auto)", "Язык интерфейса [en|ru] (по умолчанию: авто)"))
	flag.Parse()

	if *helpFlag {
		flag.Usage()
		return
	}

	if *langFlag != "" {
		SetLang(*langFlag)
	}

	if *versionFlag {
		fmt.Println("Vencord Installer Cli", buildinfo.InstallerTag, "("+buildinfo.InstallerGitHash+")")
		fmt.Println("Copyright (C) 2023 Vendicated and Vencord contributors")
		fmt.Println("License GPLv3+: GNU GPL version 3 or later <https://gnu.org/licenses/gpl.html>.")
		return
	}

	if *updateSelfFlag {
		if !<-SelfUpdateCheckDoneChan {
			die(T("Can't update self because checking for updates failed", "Не могу обновиться: проверка обновлений не удалась"))
		}
		if err := UpdateSelf(); err != nil {
			Log.Error("Failed to update self:", err)
			exitFailure()
		}
		exitSuccess()
	}

	if *locationFlag != "" && *branchFlag != "" {
		die(T("The 'location' and 'branch' flags are mutually exclusive.", "Флаги 'location' и 'branch' взаимоисключающие."))
	}

	if !isValidBranch(*branchFlag) {
		die(T("The 'branch' flag must be one of the following: [auto|stable|ptb|canary]", "Флаг 'branch' должен быть одним из: [auto|stable|ptb|canary]"))
	}

	if *installFlag || *updateFlag {
		if !<-GithubDoneChan {
			die(T("Not "+Ternary(*installFlag, "installing", "updating")+" as fetching release data failed",
				"Не "+Ternary(*installFlag, T("installing", "устанавливаю"), T("updating", "обновляю"))+": не удалось получить данные релиза"))
		}
	}

	install, uninstall, update, installOpenAsar, uninstallOpenAsar := *installFlag, *uninstallFlag, *updateFlag, *installOpenAsarFlag, *uninstallOpenAsarFlag
	switches := []*bool{&install, &update, &uninstall, &installOpenAsar, &uninstallOpenAsar}
	if !SliceContainsFunc(switches, func(b *bool) bool { return *b }) {
		interactive = true

		go func() {
			<-SelfUpdateCheckDoneChan
			if IsSelfOutdated {
				Log.Warn(T("Your installer is outdated.", "Ваш установщик устарел."))
				Log.Warn(T("To update, select the 'Update Vencord Installer' option to update, or run with --update-self", "Чтобы обновить, выберите пункт обновления или запустите с --update-self"))
			}
		}()

		choices := []string{
			T("Install Vencord", "Установить Vencord"),
			T("Repair Vencord", "Починить Vencord"),
			T("Uninstall Vencord", "Удалить Vencord"),
			T("Install OpenAsar", "Установить OpenAsar"),
			T("Uninstall OpenAsar", "Удалить OpenAsar"),
			T("View Help Menu", "Справка"),
			T("Update Vencord Installer", "Обновить установщик"),
			T("Quit", "Выход"),
		}
		_, choice, err := (&promptui.Select{
			Label: T("What would you like to do? (Press Enter to confirm)", "Что сделать? (Enter для подтверждения)"),
			Items: choices,
		}).Run()
		handlePromptError(err)

		// NB: compare by index, not by text — labels are translated (RU/EN)
		switch SliceIndex(choices, choice) {
		case 5: // View Help Menu
			flag.Usage()
			return
		case 7: // Quit
			return
		case 6: // Update Vencord Installer
			if err := UpdateSelf(); err != nil {
				Log.Error(T("Failed to update self:", "Не удалось обновиться:"), err)
				exitFailure()
			}
			exitSuccess()
		}

		*switches[SliceIndex(choices, choice)] = true
	}

	var err error
	var errSilent error
	if install {
		errSilent = PromptDiscord("patch", *locationFlag, *branchFlag).patch()
	} else if uninstall {
		errSilent = PromptDiscord("unpatch", *locationFlag, *branchFlag).unpatch()
	} else 	if update {
		Log.Info(T("Downloading latest Vencord files...", "Скачиваю последние файлы Vencord..."))
		err := installLatestBuilds()
		Log.Info(T("Done!", "Готово!"))
		if err == nil {
			errSilent = PromptDiscord("repair", *locationFlag, *branchFlag).patch()
		}
	} else if installOpenAsar {
		discord := PromptDiscord("patch", *locationFlag, *branchFlag)
		if !discord.IsOpenAsar() {
			err = discord.InstallOpenAsar()
		} else {
			die(T("OpenAsar already installed", "OpenAsar уже установлен"))
		}
	} else if uninstallOpenAsar {
		discord := PromptDiscord("patch", *locationFlag, *branchFlag)
		if discord.IsOpenAsar() {
			err = discord.UninstallOpenAsar()
		} else {
			die(T("OpenAsar not installed", "OpenAsar не установлен"))
		}
	}

	if err != nil {
		Log.Error(err)
		exitFailure()
	}
	if errSilent != nil {
		exitFailure()
	}

	exitSuccess()
}

func exit(status int) {
	if runtime.GOOS == "windows" && IsDoubleClickRun() && interactive {
		fmt.Print(T("Press Enter to exit", "Нажмите Enter для выхода"))
		var b byte
		_, _ = fmt.Scanf("%v", &b)
	}
	os.Exit(status)
}

func exitSuccess() {
	color.HiGreen(T("✔ Success!", "✔ Успешно!"))
	exit(0)
}

func exitFailure() {
	color.HiRed(T("❌ Failed!", "❌ Ошибка!"))
	exit(1)
}

func handlePromptError(err error) {
	if errors.Is(err, promptui.ErrInterrupt) {
		exit(0)
	}

	Log.FatalIfErr(err)
}

func PromptDiscord(action, dir, branch string) *DiscordInstall {
	if branch == "auto" {
		for _, b := range []string{"stable", "canary", "ptb"} {
			for _, discord := range discords {
				install := discord.(*DiscordInstall)
				if install.branch == b {
					return install
				}
			}
		}
		die(T("No Discord install found. Try manually specifying it with the --dir flag. Hint: snap is not supported", "Установка Discord не найдена. Укажите путь вручную через --dir. Подсказка: snap не поддерживается"))
	}

	if branch != "" {
		for _, discord := range discords {
			install := discord.(*DiscordInstall)
			if install.branch == branch {
				return install
			}
		}
		die(T("Discord "+branch+" not found", "Discord "+branch+" не найден"))
	}

	if dir != "" {
		if discord := ParseDiscord(dir, branch); discord != nil {
			return discord
		}

		if discord := ParseDiscordNew(dir, branch, strings.Contains(dir, "com.discordapp")); discord != nil {
			return discord
		}

		die(dir + T(" is not a valid Discord install. Hint: snap is not supported", " — не установка Discord. Подсказка: snap не поддерживается"))
	}

	items := SliceMap(discords, func(d any) string {
		install := d.(*DiscordInstall)
		//goland:noinspection GoDeprecation
		return fmt.Sprintf("%s - %s%s", strings.Title(install.branch), install.path, Ternary(install.isPatched, T(" [PATCHED]", " [ПАТЧЕН]"), ""))
	})
	customLocationLabel := T("Custom Location", "Свой путь")
	items = append(items, customLocationLabel)

	_, choice, err := (&promptui.Select{
		Label: T("Select Discord install to "+action+" (Press Enter to confirm)", "Выберите установку Discord для: "+action+" (Enter для подтверждения)"),
		Items: items,
	}).Run()
	handlePromptError(err)

	// NB: last item is always "Custom Location" — compare by index, labels are translated
	if idx := SliceIndex(items, choice); idx != len(items)-1 {
		return discords[idx].(*DiscordInstall)
	}

	for {
		custom, err := (&promptui.Prompt{
			Label: T("Custom Discord Location", "Свой путь к Discord"),
		}).Run()
		handlePromptError(err)

		if di := ParseDiscord(custom, ""); di != nil {
			return di
		}

		if di := ParseDiscordNew(custom, "", strings.Contains(custom, "com.discordapp")); di != nil {
			return di
		}

		Log.Error(T("Invalid Discord install!", "Неверная установка Discord!"))
	}
}

func InstallLatestBuilds() error {
	return installLatestBuilds()
}

func HandleScuffedInstall() {
	fmt.Println(T("Hold On!", "Стоп!"))
	fmt.Println(T("You have a broken Discord Install.", "У вас сломанная установка Discord."))
	fmt.Println(T("Please reinstall Discord before proceeding!", "Переустановите Discord перед продолжением!"))
	fmt.Println(T("Otherwise, Vencord will likely not work.", "Иначе Vencord скорее всего не будет работать."))
}
