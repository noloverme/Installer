//go:build !cli

/*
 * SPDX-License-Identifier: GPL-3.0
 * Vencord Installer, a cross platform gui/cli app for installing Vencord
 * Copyright (c) 2023 Vendicated and Vencord contributors
 */

package main

import (
	"bytes"
	_ "embed"
	"errors"
	"image"
	"image/color"

	g "github.com/AllenDang/giu"
	"github.com/AllenDang/imgui-go"

	// png decoder for icon
	_ "image/png"
	"os"
	path "path/filepath"
	"runtime"
	"strconv"
	"strings"
)

var (
	discords        []any
	radioIdx        int
	customChoiceIdx int

	customDir              string
	autoCompleteDir        string
	autoCompleteFile       string
	autoCompleteCandidates []string
	autoCompleteIdx        int
	lastAutoComplete       string
	didAutoComplete        bool

	modalId      = 0
	modalTitle   = T("Oh No :(", "Ой :(")
	modalMessage = T("You should never see this", "Вы не должны это видеть")
	modalExtra   = ""

	acceptedOpenAsar   bool
	showedUpdatePrompt bool
	showCustomLocation bool

	win *g.MasterWindow
)

//go:embed winres/icon.png
var iconBytes []byte

func init() {
	LogLevel = LevelDebug
}

func main() {
	InitGithubDownloader()
	discords = FindDiscords()
	customChoiceIdx = len(discords)

	go func() {
		<-GithubDoneChan
		g.Update()
	}()

	go func() {
		<-SelfUpdateCheckDoneChan
		g.Update()
	}()

	win = g.NewMasterWindow("Vencord Installer", 1200, 800, 0)

	icon, _, err := image.Decode(bytes.NewReader(iconBytes))
	if err != nil {
		Log.Warn("Failed to load application icon", err)
		Log.Debug(iconBytes, len(iconBytes))
	} else {
		win.SetIcon([]image.Image{icon})
	}

	win.Run(loop)
}

type CondWidget struct {
	predicate  bool
	ifWidget   func() g.Widget
	elseWidget func() g.Widget
}

func (w *CondWidget) Build() {
	if w.predicate {
		w.ifWidget().Build()
	} else if w.elseWidget != nil {
		w.elseWidget().Build()
	}
}

func getChosenInstall() *DiscordInstall {
	if radioIdx != customChoiceIdx {
		return discords[radioIdx].(*DiscordInstall)
	}

	if discord := ParseDiscord(customDir, ""); discord != nil {
		return discord
	}

	if discord := ParseDiscordNew(customDir, "", strings.Contains(customDir, "com.discordapp")); discord != nil {
		return discord
	}

	g.OpenPopup("#invalid-custom-location")
	return nil
}

func InstallLatestBuilds() (err error) {
	if IsDevInstall {
		return
	}

	err = installLatestBuilds()
	if err != nil {
		ShowModal(
			T("Failed to install the latest Vencord builds from GitHub", "Не удалось установить последние сборки Vencord с GitHub"),
			T("If this issue persists, visit https://vencord.dev/support for help.", "Если проблема сохраняется, перейдите на https://vencord.dev/support за помощью."),
			err.Error(),
		)
	}
	return
}

func handlePatch() {
	choice := getChosenInstall()
	if choice != nil {
		choice.Patch()
	}
}

func handleUnpatch() {
	choice := getChosenInstall()
	if choice != nil {
		choice.Unpatch()
	}
}

func handleOpenAsar() {
	if acceptedOpenAsar || getChosenInstall().IsOpenAsar() {
		handleOpenAsarConfirmed()
		return
	}

	g.OpenPopup("#openasar-confirm")
}

func handleOpenAsarConfirmed() {
	choice := getChosenInstall()
	if choice != nil {
		if choice.IsOpenAsar() {
			if err := choice.UninstallOpenAsar(); err != nil {
				handleErr(choice, err, "uninstall OpenAsar from")
			} else {
				g.OpenPopup("#openasar-unpatched")
				g.Update()
			}
		} else {
			if err := choice.InstallOpenAsar(); err != nil {
				handleErr(choice, err, "install OpenAsar on")
			} else {
				g.OpenPopup("#openasar-patched")
				g.Update()
			}
		}
	}
}

func handleErr(di *DiscordInstall, err error, action string) {
	if errors.Is(err, os.ErrPermission) {
		switch runtime.GOOS {
		case "windows":
			err = errors.New(T("Permission denied. Make sure your Discord is fully closed (from the tray)!", "Отказано в доступе. Убедитесь, что Discord полностью закрыт (включая трей)!"))
		case "darwin":
			HandleInsufficientPermissions()
			return
		default:
			err = errors.New(T("Permission denied. Maybe try running me as Administrator/Root?", "Отказано в доступе. Попробуйте запустить от имени администратора/root?"))
		}
	}

	ShowModal(
		T("Failed to "+action+" this Install.", "Не удалось выполнить операцию ("+action+")."),
		T("If this issue persists, visit: https://vencord.dev/support", "Если проблема сохраняется, перейдите на: https://vencord.dev/support"),
		err.Error(),
	)
}

func HandleScuffedInstall() {
	g.OpenPopup("#scuffed-install")
}

func HandleInsufficientPermissions() {
	g.OpenPopup("#insufficient-permissions")
}

func (di *DiscordInstall) Patch() {
	if CheckScuffedInstall() {
		return
	}
	if err := di.patch(); err != nil {
		handleErr(di, err, "patch")
	} else {
		g.OpenPopup("#patched")
	}
}

func (di *DiscordInstall) Unpatch() {
	if err := di.unpatch(); err != nil {
		handleErr(di, err, "unpatch")
	} else {
		g.OpenPopup("#unpatched")
	}
}

func onCustomInputChanged() {
	p := customDir
	if len(p) != 0 {
		// Select the custom option for people
		radioIdx = customChoiceIdx
	}

	dir := path.Dir(p)

	isNewDir := strings.HasSuffix(p, "/")
	wentUpADir := !isNewDir && dir != autoCompleteDir

	if isNewDir || wentUpADir {
		autoCompleteDir = dir
		// reset all the funnies
		autoCompleteIdx = 0
		lastAutoComplete = ""
		autoCompleteFile = ""
		autoCompleteCandidates = nil

		// Generate autocomplete items
		files, err := os.ReadDir(dir)
		if err == nil {
			for _, file := range files {
				autoCompleteCandidates = append(autoCompleteCandidates, file.Name())
			}
		}
	} else if !didAutoComplete {
		// reset auto complete and update our file
		autoCompleteFile = path.Base(p)
		lastAutoComplete = ""
	}

	if wentUpADir {
		autoCompleteFile = path.Base(p)
	}

	didAutoComplete = false
}

// go can you give me []any?
// to pass to giu RangeBuilder?
// yeeeeees
// actually returns []string like a boss
func makeAutoComplete() []any {
	input := strings.ToLower(autoCompleteFile)

	var candidates []any
	for _, e := range autoCompleteCandidates {
		file := strings.ToLower(e)
		if autoCompleteFile == "" || strings.HasPrefix(file, input) {
			candidates = append(candidates, e)
		}
	}
	return candidates
}

func makeRadioOnChange(i int) func() {
	return func() {
		radioIdx = i
	}
}

func renderFilesDirErr() g.Widget {
	return g.Layout{
		g.Dummy(0, 50),
		g.Style().
			SetColor(g.StyleColorText, DiscordRed).
			SetFontSize(30).
			To(
				g.Align(g.AlignCenter).To(
					g.Label(T("Error: Failed to create: ", "Ошибка: не удалось создать: ")+FilesDirErr.Error()),
					g.Label(T("Resolve this error, then restart me!", "Исправьте эту ошибку и перезапустите меня!")),
				),
			),
	}
}

func Tooltip(label string) g.Widget {
	return g.Style().
		SetStyle(g.StyleVarWindowPadding, 10, 8).
		SetStyleFloat(g.StyleVarWindowRounding, 8).
		To(
			g.Tooltip(label),
		)
}

func InfoModal(id, title, description string) g.Widget {
	return RawInfoModal(id, title, description, "", false)
}

func InfoModalExtra(id, title, description, extra string) g.Widget {
	return RawInfoModal(id, title, description, extra, false)
}

func RawInfoModal(id, title, description, extra string, isOpenAsar bool) g.Widget {
	isDynamic := strings.HasPrefix(id, "#modal") && extra != ""
	return g.Style().
		SetStyle(g.StyleVarWindowPadding, 30, 30).
		SetStyleFloat(g.StyleVarWindowRounding, 12).
		To(
			g.PopupModal(id).
				Flags(g.WindowFlagsNoTitleBar|Ternary(isDynamic, g.WindowFlagsAlwaysAutoResize, 0)).
				Layout(
					g.Style().SetFontSize(30).To(
						g.Label(title),
					),
					g.Dummy(0, 5),
					g.Style().SetFontSize(20).To(
						g.Label(description),
					),
					&CondWidget{extra != "", func() g.Widget {
						return g.Column(
							g.Dummy(0, 10),
							g.Style().SetFontSize(20).To(
								g.Label(extra).Wrapped(true),
							),
						)
					}, nil},
					&CondWidget{id == "#scuffed-install", func() g.Widget {
						return g.Column(
							g.Dummy(0, 10),
							g.Button(T("Take me there!", "Открыть папку!")).OnClick(func() {
								// this issue only exists on windows so using Windows specific path is oki
								username := os.Getenv("USERNAME")
								programData := os.Getenv("PROGRAMDATA")
								g.OpenURL("file://" + path.Join(programData, username))
							}).Size(200, 30),
						)
					}, nil},
					g.Dummy(0, 20),
					&CondWidget{id == "#insufficient-permissions", func() g.Widget {
						return g.Column(
							g.Dummy(0, 10),
							g.Button(T("Open Settings", "Открыть настройки")).OnClick(func() {
								// "App Management" permissions doesnt exist on Monterey, just use full disk access for now
								g.OpenURL("x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles")
							}).Size(200, 30),
						)
					}, nil},
					&CondWidget{isOpenAsar,
						func() g.Widget {
							return g.Row(
								g.Button(T("Accept", "Принять")).
									OnClick(func() {
										acceptedOpenAsar = true
										g.CloseCurrentPopup()
									}).
									Size(100, 30),
								g.Button(T("Cancel", "Отмена")).
									OnClick(func() {
										g.CloseCurrentPopup()
									}).
									Size(100, 30),
							)
						},
						func() g.Widget {
							return g.Button(T("Ok", "ОК")).
								OnClick(func() {
									g.CloseCurrentPopup()
								}).
								Size(100, 30)
						},
					},
				),
		)
}

func UpdateModal() g.Widget {
	return g.Style().
		SetStyle(g.StyleVarWindowPadding, 30, 30).
		SetStyleFloat(g.StyleVarWindowRounding, 12).
		To(
			g.PopupModal("#update-prompt").
				Flags(g.WindowFlagsNoTitleBar | g.WindowFlagsAlwaysAutoResize).
				Layout(
				g.Align(g.AlignCenter).To(
					g.Style().SetFontSize(30).To(
						g.Label(T("Your Installer is outdated!", "Установщик устарел!")),
					),
					g.Style().SetFontSize(20).To(
						g.Label(
							T("Would you like to update now?\n\n"+
								"Once you press Update Now, the new installer will automatically be downloaded.\n"+
								"The installer will temporarily seem unresponsive. Just wait!\n"+
								"Once the update is done, the Installer will automatically reopen.\n\n"+
								"On MacOs, Auto updates are not supported, so it will instead open in browser.",
								"Обновить сейчас?\n\n"+
									"После нажатия «Обновить» новый установщик скачается автоматически.\n"+
									"Установщик временно перестанет отвечать — просто подождите!\n"+
									"После обновления установщик перезапустится сам.\n\n"+
									"На MacOS автообновление не поддерживается, вместо этого откроется браузер."),
						),
					),
					g.Row(
						g.Button(T("Update Now", "Обновить")).
							OnClick(func() {
								if runtime.GOOS == "darwin" {
									g.CloseCurrentPopup()
									g.OpenURL(GetInstallerDownloadLink())
									return
								}

								err := UpdateSelf()
								g.CloseCurrentPopup()

								if err != nil {
									ShowModal(
										T("Failed to update self!", "Не удалось обновиться!"),
										T("Please manually download the latest Installer.", "Пожалуйста, скачайте последнюю версию установщика вручную."),
										err.Error(),
									)
								} else {
									if err = RelaunchSelf(); err != nil {
										ShowModal(
											T("Failed to restart self!", "Не удалось перезапуститься!"),
											T("Please manually restart the Installer.", "Пожалуйста, перезапустите установщик вручную."),
											err.Error(),
										)
									}
								}
							}).
							Size(100, 30),
						g.Button(T("Later", "Позже")).
							OnClick(func() {
								g.CloseCurrentPopup()
							}).
							Size(100, 30),
					),
					),
				),
		)
}

func ShowModal(title, desc, extra string) {
	modalTitle = title
	modalMessage = desc
	modalExtra = extra
	modalId++
	g.OpenPopup("#modal" + strconv.Itoa(modalId))
}

func renderInstaller() g.Widget {
	candidates := makeAutoComplete()

	wi, _ := win.GetSize()
	w := float32(wi) - 96

	var currentDiscord *DiscordInstall
	if radioIdx != customChoiceIdx {
		currentDiscord = discords[radioIdx].(*DiscordInstall)
	}
	var isOpenAsar = currentDiscord != nil && currentDiscord.IsOpenAsar()

	if CanUpdateSelf() && !showedUpdatePrompt {
		showedUpdatePrompt = true
		g.OpenPopup("#update-prompt")
	}

	layout := g.Layout{
		g.Row(
			g.Label(T("Language / Язык:", "Язык / Language:")),
			g.RadioButton("English", GetLang() == LangEN).OnChange(func() { SetLang(LangEN) }),
			g.RadioButton("Русский", GetLang() == LangRU).OnChange(func() { SetLang(LangRU) }),
		),

		g.Dummy(0, 10),

		g.Style().SetFontSize(20).To(
			renderErrorCard(
				DiscordYellow,
				color.Black,
				T("**Github** and **vencord.dev** are the only official places to get Vencord. Any other site claiming to be us is malicious.\n"+
					"If you downloaded from any other source, you should delete / uninstall everything immediately, run a malware scan and change your Discord password.",
					"**Github** и **vencord.dev** — единственные официальные источники Vencord. Любой другой сайт от нашего имени — вредоносный.\n"+
						"Если вы скачали из другого источника, немедленно всё удалите, проверьте компьютер антивирусом и смените пароль Discord."),
				90,
			),
		),

		g.Dummy(0, 20),

		g.Style().SetFontSize(30).To(
			g.Label(T("Please select an install to patch", "Выберите установку для патча")),
		),
		g.Dummy(0, 10),

		&CondWidget{len(discords) == 0, func() g.Widget {
			s := T("No Discord installs found. You first need to install Discord.", "Установки Discord не найдены. Сначала установите Discord.")
			if runtime.GOOS == "linux" {
				s += T(" snap is not supported.", " snap не поддерживается.")
			}

			return &CondWidget{!showCustomLocation, func() g.Widget {
				return g.Column(
					g.Style().SetFontSize(25).To(g.Label(s)),
					g.Dummy(0, 10),
					g.Checkbox("I am an advanced user and have Discord installed at a different location", &showCustomLocation),
				)
			}, nil}
		}, nil},

		g.Style().SetFontSize(20).To(
			g.RangeBuilder("Discords", discords, func(i int, v any) g.Widget {
				d := v.(*DiscordInstall)
				var text string
				switch d.branch {
				case "ptb":
					text = "Discord PTB"
				case "canary":
					text = "Discord Canary"
				case "development":
					text = "Discord Development"
				default:
					text = "Discord"
				}

			if d.isPatched {
				text += T(" (Vencord Installed)", " (Vencord установлен)")
			}

				return g.Row(
					g.RadioButton(text, radioIdx == i).OnChange(makeRadioOnChange(i)),
					g.Style().SetColor(g.StyleColorText, color.RGBA{0xff, 0xff, 0xff, 0x80}).To(g.Label(" "+d.path)),
				)
			}),
			&CondWidget{showCustomLocation, func() g.Widget {
				return g.RadioButton("Custom Install Location", radioIdx == customChoiceIdx).OnChange(makeRadioOnChange(customChoiceIdx))
			}, nil},
		),

		&CondWidget{showCustomLocation, func() g.Widget {
			return g.Column(
				g.Dummy(0, 5),
				g.Style().
					SetStyle(g.StyleVarFramePadding, 16, 16).
					SetFontSize(20).
					To(
						g.InputText(&customDir).Hint("The custom location").
							Size(w-16).
							Flags(g.InputTextFlagsCallbackCompletion).
							OnChange(onCustomInputChanged).
							// this library has its own autocomplete but it's broken
							Callback(
								func(data imgui.InputTextCallbackData) int32 {
									if len(candidates) == 0 {
										return 0
									}
									// just wrap around
									if autoCompleteIdx >= len(candidates) {
										autoCompleteIdx = 0
									}

									// used by change handler
									didAutoComplete = true

									start := len(customDir)
									// Delete previous auto complete
									if lastAutoComplete != "" {
										start -= len(lastAutoComplete)
										data.DeleteBytes(start, len(lastAutoComplete))
									} else if autoCompleteFile != "" { // delete partial input
										start -= len(autoCompleteFile)
										data.DeleteBytes(start, len(autoCompleteFile))
									}

									// Insert auto complete
									lastAutoComplete = candidates[autoCompleteIdx].(string)
									data.InsertBytes(start, []byte(lastAutoComplete))
									autoCompleteIdx++

									return 0
								},
							),
					),
				g.RangeBuilder("AutoComplete", candidates, func(i int, v any) g.Widget {
					dir := v.(string)
					return g.Label(dir)
				}),
			)
		}, nil},

		g.Dummy(0, 20),

		g.Style().SetFontSize(20).To(
			g.Row(
				g.Style().
					SetColor(g.StyleColorButton, DiscordGreen).
					SetColor(g.StyleColorButtonHovered, DiscordGreenHovered).
					SetStyle(g.StyleVarFrameRounding, 8, 8).
					SetDisabled(GithubError != nil).
				To(
					g.Button(T("Install", "Установить")).
						OnClick(handlePatch).
						Size((w-40)/4, 50),
					Tooltip(T("Patch the selected Discord Install", "Пропатчить выбранную установку Discord")),
				),
				g.Style().
					SetColor(g.StyleColorButton, DiscordBlue).
					SetColor(g.StyleColorButtonHovered, DiscordBlueHovered).
					SetStyle(g.StyleVarFrameRounding, 8, 8).
					SetDisabled(GithubError != nil).
				To(
					g.Button(T("Reinstall / Repair", "Переустановить / Починить")).
						OnClick(func() {
							if IsDevInstall {
								handlePatch()
							} else {
								err := InstallLatestBuilds()
								if err == nil {
									handlePatch()
								}
							}
						}).
						Size((w-40)/4, 50),
					Tooltip(T("Reinstall & Update Vencord", "Переустановить и обновить Vencord")),
				),
				g.Style().
					SetColor(g.StyleColorButton, DiscordRed).
					SetColor(g.StyleColorButtonHovered, DiscordRedHovered).
					SetStyle(g.StyleVarFrameRounding, 8, 8).
					To(
						g.Button(T("Uninstall", "Удалить")).
							OnClick(handleUnpatch).
							Size((w-40)/4, 50),
						Tooltip(T("Unpatch the selected Discord Install", "Откатить патч выбранной установки Discord")),
					),
				g.Style().
					SetColor(g.StyleColorButton, Ternary(isOpenAsar, DiscordRed, DiscordGreen)).
					SetColor(g.StyleColorButtonHovered, Ternary(isOpenAsar, DiscordRedHovered, DiscordGreenHovered)).
					SetStyle(g.StyleVarFrameRounding, 8, 8).
					To(
						g.Button(Ternary(isOpenAsar, T("Uninstall OpenAsar", "Удалить OpenAsar"), Ternary(currentDiscord != nil, T("Install OpenAsar", "Установить OpenAsar"), T("(Un-)Install OpenAsar", "(Де-)Установка OpenAsar")))).
							OnClick(handleOpenAsar).
							Size((w-40)/4, 50),
						Tooltip(T("Manage OpenAsar", "Управление OpenAsar")),
					),
			),
		),

		InfoModal("#patched", T("Installed!", "Установлено!"), T("Vencord was successfully installed!", "Vencord успешно установлен!")),
		InfoModal("#unpatched", T("Uninstalled", "Удалено"), T("Vencord has been uninstalled!", "Vencord удалён!")),
		InfoModal("#scuffed-install", T("Hold On!", "Стоп!"), T("You have a broken Discord Install.\n"+
			"Sometimes Discord decides to install to the wrong location for some reason!\n"+
			"You need to fix this before patching, otherwise Vencord will likely not work.\n\n"+
			"Use the below button to jump there and delete any folder called Discord or Squirrel.\n"+
			"If the folder is now empty, feel free to go back a step and delete that folder too.\n"+
			"Then see if Discord still starts. If not, reinstall it",
			"У вас сломанная установка Discord.\n"+
				"Иногда Discord ставится не в ту папку!\n"+
				"Исправьте это перед патчем, иначе Vencord скорее всего не будет работать.\n\n"+
				"Кнопкой ниже откройте папку и удалите всё с именем Discord или Squirrel.\n"+
				"Если папка стала пустой, можно удалить и её.\n"+
				"Проверьте, запускается ли Discord. Если нет — переустановите его")),
		RawInfoModal("#openasar-confirm", "OpenAsar", T("OpenAsar is an open-source alternative of Discord desktop's app.asar.\n"+
			"Vencord is in no way affiliated with OpenAsar.\n"+
			"You're installing OpenAsar at your own risk. If you run into issues with OpenAsar,\n"+
			"no support will be provided, join the OpenAsar Server instead!\n\n"+
			"To install OpenAsar, press Accept and click 'Install OpenAsar' again.",
			"OpenAsar — открытая альтернатива app.asar десктопного Discord.\n"+
				"Vencord никак не связан с OpenAsar.\n"+
				"Вы ставите OpenAsar на свой риск. При проблемах с OpenAsar\n"+
				"поддержка не оказывается — обращайтесь на сервер OpenAsar!\n\n"+
				"Чтобы установить OpenAsar, нажмите «Принять» и ещё раз «Установить OpenAsar»."), "", true),
		InfoModal("#insufficient-permissions", T("Insufficient Permissions", "Недостаточно прав"), T("Permission denied. Please grant the installer permissions in the settings.", "Отказано в доступе. Выдайте установщику разрешения в настройках.")),
		InfoModal("#openasar-patched", T("Successfully Installed OpenAsar", "OpenAsar успешно установлен"), T("If Discord is still open, fully close it first. Then start it again and verify OpenAsar installed successfully!", "Если Discord ещё открыт, полностью закройте его. Затем запустите снова и проверьте, что OpenAsar встал!")),
		InfoModal("#openasar-unpatched", T("Successfully Uninstalled OpenAsar", "OpenAsar успешно удалён"), T("If Discord is still open, fully close it first. Then start it again and it should be back to stock!", "Если Discord ещё открыт, полностью закройте его. Затем запустите снова — всё должно стать как было!")),
		InfoModal("#invalid-custom-location", T("Invalid Location", "Неверный путь"), T("The specified location is not a valid Discord install.\nMake sure you select the base folder.\n\nHint: Discord snap is not supported. use flatpak or .deb",
			"Указанный путь — не установка Discord.\nВыберите базовую папку.\n\nПодсказка: snap Discord не поддерживается, используйте flatpak или .deb")),
		InfoModalExtra("#modal"+strconv.Itoa(modalId), modalTitle, modalMessage, modalExtra),

		UpdateModal(),
	}

	return layout
}

func renderErrorCard(bgColor color.Color, textColor color.Color, message string, height float32) g.Widget {
	return g.Style().
		SetColor(g.StyleColorChildBg, bgColor).
		SetStyleFloat(g.StyleVarAlpha, 0.9).
		SetStyle(g.StyleVarWindowPadding, 10, 10).
		SetStyleFloat(g.StyleVarChildRounding, 5).
		To(
			g.Child().
				Size(g.Auto, height).
				Layout(
					g.Row(
						g.Style().SetColor(g.StyleColorText, textColor).To(
							g.Markdown(&message),
						),
					),
				),
		)
}

func loop() {
	g.PushWindowPadding(48, 48)

	g.SingleWindow().
		RegisterKeyboardShortcuts(
			g.WindowShortcut{Key: g.KeyUp, Callback: func() {
				if radioIdx > 0 {
					radioIdx--
				}
			}},
			g.WindowShortcut{Key: g.KeyDown, Callback: func() {
				if radioIdx < customChoiceIdx {
					radioIdx++
				}
			}},
		).
		Layout(
			g.Align(g.AlignCenter).To(
				g.Style().SetFontSize(40).To(
					g.Label("Vencord Installer"),
				),
			),
			g.Dummy(0, 40),

		&CondWidget{
			GithubError != nil,
			func() g.Widget {
				return g.Style().SetFontSize(20).To(renderErrorCard(
					DiscordRed,
					color.White,
					T(
						"Failed to fetch Info from GitHub. If this issue persists, visit https://vencord.dev/support for help.",
						"Не удалось получить данные с GitHub. Если проблема сохраняется, обратитесь: https://vencord.dev/support",
					),
					40,
				))
			},
			nil,
		},

			&CondWidget{
				predicate:  FilesDirErr != nil,
				ifWidget:   renderFilesDirErr,
				elseWidget: renderInstaller,
			},
		)

	g.PopStyle()
}
