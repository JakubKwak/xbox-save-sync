package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	appSync "xbox-save-sync/internal/sync"
)

// UI colors (Xbox / Dark theme)
var (
	colorBg         = color.NRGBA{R: 0x12, G: 0x14, B: 0x17, A: 0xFF}
	colorCard       = color.NRGBA{R: 0x1E, G: 0x22, B: 0x27, A: 0xFF}
	colorCardBorder = color.NRGBA{R: 0x2D, G: 0x33, B: 0x3B, A: 0xFF}
	colorXboxGreen  = color.NRGBA{R: 0x10, G: 0x7C, B: 0x10, A: 0xFF}
	colorGreenHover = color.NRGBA{R: 0x16, G: 0x98, B: 0x16, A: 0xFF}
	colorBlue       = color.NRGBA{R: 0x00, G: 0x78, B: 0xD4, A: 0xFF}
	colorOrange     = color.NRGBA{R: 0xD8, G: 0x6E, B: 0x00, A: 0xFF}
	colorRed        = color.NRGBA{R: 0xD1, G: 0x34, B: 0x38, A: 0xFF}
	colorText       = color.NRGBA{R: 0xEA, G: 0xEA, B: 0xEB, A: 0xFF}
	colorTextMuted  = color.NRGBA{R: 0x8B, G: 0x94, B: 0x9E, A: 0xFF}
)

type App struct {
	syncer   *appSync.Syncer
	saveDir  string
	bucket   string
	prefix   string
	region   string
	configOK bool

	window *app.Window
	theme  *material.Theme

	mu             sync.Mutex
	isSyncing      bool
	statusMsg      string
	statusIsError  bool
	currentGame    string
	logs           []string
	activeConflict *appSync.ConflictInfo
	conflictChan   chan appSync.ConflictChoice

	// Widgets
	btnSync         widget.Clickable
	btnClearLogs    widget.Clickable
	btnChoiceCloud  widget.Clickable
	btnChoiceLocal  widget.Clickable
	btnChoiceSkip   widget.Clickable
	logList         widget.List
}

func New(syncer *appSync.Syncer, saveDir, bucket, prefix, region string) *App {
	th := material.NewTheme()
	th.Palette.Bg = colorBg
	th.Palette.Fg = colorText
	th.Palette.ContrastBg = colorXboxGreen
	th.Palette.ContrastFg = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}

	configOK := bucket != "" && saveDir != ""

	a := &App{
		syncer:       syncer,
		saveDir:      saveDir,
		bucket:       bucket,
		prefix:       prefix,
		region:       region,
		configOK:     configOK,
		theme:        th,
		statusMsg:    "Ready to sync",
		logList:      widget.List{List: layout.List{Axis: layout.Vertical, ScrollToEnd: true}},
		logs:         make([]string, 0),
		conflictChan: make(chan appSync.ConflictChoice, 1),
	}

	if syncer != nil {
		syncer.SetLogger(func(format string, args ...any) {
			msg := fmt.Sprintf(format, args...)
			fmt.Println(msg)
			a.addLog(msg)
		})

		syncer.SetProgressCallback(func(gameTitle, action string) {
			fmt.Printf("[%s] %s\n", gameTitle, action)
			a.mu.Lock()
			a.currentGame = fmt.Sprintf("%s (%s)", gameTitle, action)
			a.mu.Unlock()
			if a.window != nil {
				a.window.Invalidate()
			}
		})

		syncer.SetConflictResolver(func(info appSync.ConflictInfo) (appSync.ConflictChoice, error) {
			return a.promptConflict(info)
		})
	} else if configOK {
		a.statusMsg = "Failed to initialize syncer"
		a.statusIsError = true
		a.addLogLocked("ERROR: Failed to initialize AWS S3 syncer")
	}

	return a
}

func (a *App) SetInitError(err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	a.statusMsg = fmt.Sprintf("Initialization error: %v", err)
	a.statusIsError = true
	a.addLogLocked(fmt.Sprintf("ERROR: %v", err))
	a.mu.Unlock()
	if a.window != nil {
		a.window.Invalidate()
	}
}

func (a *App) addLogLocked(msg string) {
	timestamp := time.Now().Format("15:04:05")
	a.logs = append(a.logs, fmt.Sprintf("[%s] %s", timestamp, msg))
	// Keep maximum 200 log messages in memory
	if len(a.logs) > 200 {
		a.logs = a.logs[len(a.logs)-200:]
	}
}

func (a *App) addLog(msg string) {
	a.mu.Lock()
	a.addLogLocked(msg)
	a.mu.Unlock()

	if a.window != nil {
		a.window.Invalidate()
	}
}

func (a *App) promptConflict(info appSync.ConflictInfo) (appSync.ConflictChoice, error) {
	a.mu.Lock()
	a.activeConflict = &info
	a.statusMsg = fmt.Sprintf("Conflict detected for %s", info.Title)
	a.statusIsError = true
	a.mu.Unlock()

	if a.window != nil {
		a.window.Invalidate()
	}

	// Wait for user choice from UI
	choice := <-a.conflictChan

	a.mu.Lock()
	a.activeConflict = nil
	a.statusMsg = "Syncing in progress..."
	a.statusIsError = false
	a.mu.Unlock()

	if a.window != nil {
		a.window.Invalidate()
	}

	return choice, nil
}

func (a *App) Run() error {
	w := new(app.Window)
	w.Option(
		app.Title("Xbox Save Sync"),
		app.Size(unit.Dp(960), unit.Dp(640)),
		app.MinSize(unit.Dp(600), unit.Dp(450)),
	)
	a.window = w

	// Automatically start syncing on launch if configuration is valid and syncer initialized
	if a.configOK && a.syncer != nil {
		a.startSync()
	}

	var ops op.Ops

	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err

		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (a *App) startSync() {
	a.mu.Lock()
	if a.isSyncing || a.syncer == nil || !a.configOK {
		a.mu.Unlock()
		return
	}
	a.isSyncing = true
	a.statusMsg = "Syncing in progress..."
	a.statusIsError = false
	a.currentGame = "Starting..."
	a.addLogLocked("Starting sync...")
	a.mu.Unlock()

	fmt.Println("Starting sync...")

	if a.window != nil {
		a.window.Invalidate()
	}

	go func() {
		err := a.syncer.Sync()

		a.mu.Lock()
		a.isSyncing = false
		a.currentGame = ""
		if err != nil {
			a.statusMsg = fmt.Sprintf("Sync failed: %v", err)
			a.statusIsError = true
			a.addLogLocked(fmt.Sprintf("ERROR: %v", err))
			fmt.Printf("Sync failed: %v\n", err)
		} else {
			a.statusMsg = "All saves are up to date!"
			a.statusIsError = false
			a.addLogLocked("Sync completed successfully.")
			fmt.Println("Sync completed successfully.")
		}
		a.mu.Unlock()

		if a.window != nil {
			a.window.Invalidate()
		}
	}()
}

func (a *App) layout(gtx layout.Context) layout.Dimensions {
	// Handle button clicks
	if a.btnSync.Clicked(gtx) {
		a.startSync()
	}
	if a.btnClearLogs.Clicked(gtx) {
		a.mu.Lock()
		a.logs = a.logs[:0]
		a.mu.Unlock()
	}

	// Fill background
	paint.Fill(gtx.Ops, colorBg)

	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{
			Axis:      layout.Vertical,
			Alignment: layout.Start,
		}.Layout(gtx,
			// Header
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return a.layoutHeader(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

			// Config Summary bar
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return a.layoutConfigBar(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

			// Conflict dialog if active, else Main Action & Status
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				a.mu.Lock()
				conflict := a.activeConflict
				a.mu.Unlock()

				if conflict != nil {
					return a.layoutConflictDialog(gtx, conflict)
				}
				return a.layoutSyncControl(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

			// Logs list (takes remaining space)
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return a.layoutLogsCard(gtx)
			}),
		)
	})
}

func (a *App) layoutHeader(gtx layout.Context) layout.Dimensions {
	return layout.Flex{
		Axis:      layout.Horizontal,
		Alignment: layout.Middle,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			title := material.H5(a.theme, "Xbox Save Sync")
			title.Color = colorText
			title.Font.Weight = font.Bold
			return title.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			sub := material.Body2(a.theme, "Steam Deck & PC S3 Cloud Saves")
			sub.Color = colorTextMuted
			return sub.Layout(gtx)
		}),
	)
}

func (a *App) layoutConfigBar(gtx layout.Context) layout.Dimensions {
	return drawCard(gtx, colorCard, colorCardBorder, unit.Dp(8), layout.UniformInset(unit.Dp(12)), func(gtx layout.Context) layout.Dimensions {
		if !a.configOK {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Body1(a.theme, "⚠️ Configuration Missing: Check your .env file!")
					lbl.Color = colorOrange
					lbl.Font.Weight = font.Bold
					return lbl.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(a.theme, "Please ensure SAVE_DIR and AWS_S3_BUCKET are configured in .env")
					lbl.Color = colorTextMuted
					return lbl.Layout(gtx)
				}),
			)
		}

		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(a.theme, "LOCAL SAVE DIRECTORY")
						lbl.Color = colorTextMuted
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(a.theme, truncateStr(a.saveDir, 60))
						lbl.Color = colorText
						return lbl.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				bucketText := a.bucket
				if a.prefix != "" {
					bucketText += "/" + a.prefix
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(a.theme, "S3 BUCKET & PREFIX")
						lbl.Color = colorTextMuted
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(a.theme, truncateStr(bucketText, 40))
						lbl.Color = colorText
						return lbl.Layout(gtx)
					}),
				)
			}),
		)
	})
}

func (a *App) layoutSyncControl(gtx layout.Context) layout.Dimensions {
	return drawCard(gtx, colorCard, colorCardBorder, unit.Dp(8), layout.UniformInset(unit.Dp(16)), func(gtx layout.Context) layout.Dimensions {
		a.mu.Lock()
		syncing := a.isSyncing
		statusText := a.statusMsg
		isErr := a.statusIsError
		current := a.currentGame
		a.mu.Unlock()

		return layout.Flex{
			Axis:      layout.Horizontal,
			Alignment: layout.Middle,
		}.Layout(gtx,
			// Big Sync Button (touch-friendly for Steam Deck)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				var btn material.ButtonStyle
				if syncing {
					btn = material.Button(a.theme, &a.btnSync, "Syncing...")
					btn.Background = colorTextMuted
				} else {
					btn = material.Button(a.theme, &a.btnSync, "Sync Saves Now")
					btn.Background = colorXboxGreen
				}
				btn.TextSize = unit.Sp(17)
				btn.Inset = layout.Inset{
					Top:    unit.Dp(14),
					Bottom: unit.Dp(14),
					Left:   unit.Dp(24),
					Right:  unit.Dp(24),
				}
				return btn.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(20)}.Layout),

			// Status Description
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body1(a.theme, statusText)
						lbl.Font.Weight = font.Bold
						if isErr {
							lbl.Color = colorRed
						} else if syncing {
							lbl.Color = colorXboxGreen
						} else {
							lbl.Color = colorText
						}
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if current == "" {
							return layout.Dimensions{}
						}
						lbl := material.Caption(a.theme, current)
						lbl.Color = colorTextMuted
						return lbl.Layout(gtx)
					}),
				)
			}),
		)
	})
}

func (a *App) layoutConflictDialog(gtx layout.Context, info *appSync.ConflictInfo) layout.Dimensions {
	if a.btnChoiceCloud.Clicked(gtx) {
		select {
		case a.conflictChan <- appSync.ChoiceDownload:
		default:
		}
	}
	if a.btnChoiceLocal.Clicked(gtx) {
		select {
		case a.conflictChan <- appSync.ChoiceUpload:
		default:
		}
	}
	if a.btnChoiceSkip.Clicked(gtx) {
		select {
		case a.conflictChan <- appSync.ChoiceSkip:
		default:
		}
	}

	return drawCard(gtx, color.NRGBA{R: 0x2A, G: 0x1E, B: 0x14, A: 0xFF}, colorOrange, unit.Dp(10), layout.UniformInset(unit.Dp(16)), func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{
			Axis: layout.Vertical,
		}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.H6(a.theme, fmt.Sprintf("⚠️ Conflict: %s", info.Title))
				lbl.Color = colorOrange
				lbl.Font.Weight = font.Bold
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				txt := fmt.Sprintf("Both cloud and local saves were modified independently.\n• Cloud uploaded: %s\n• Local modified: %s",
					info.CloudTime.Local().Format(time.RFC1123),
					info.LocalTime.Local().Format(time.RFC1123),
				)
				lbl := material.Body2(a.theme, txt)
				lbl.Color = colorText
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),

			// Large Touch Choice Buttons for Steam Deck
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(a.theme, &a.btnChoiceCloud, "Download Cloud Save")
						btn.Background = colorBlue
						btn.TextSize = unit.Sp(15)
						btn.Inset = layout.UniformInset(unit.Dp(12))
						return btn.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(a.theme, &a.btnChoiceLocal, "Upload Local Save")
						btn.Background = colorOrange
						btn.TextSize = unit.Sp(15)
						btn.Inset = layout.UniformInset(unit.Dp(12))
						return btn.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(a.theme, &a.btnChoiceSkip, "Skip Game")
						btn.Background = colorTextMuted
						btn.TextSize = unit.Sp(15)
						btn.Inset = layout.UniformInset(unit.Dp(12))
						return btn.Layout(gtx)
					}),
				)
			}),
		)
	})
}

func (a *App) layoutLogsCard(gtx layout.Context) layout.Dimensions {
	return drawCard(gtx, colorCard, colorCardBorder, unit.Dp(8), layout.UniformInset(unit.Dp(12)), func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{
			Axis: layout.Vertical,
		}.Layout(gtx,
			// Logs header with Clear button
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{
					Axis:      layout.Horizontal,
					Alignment: layout.Middle,
				}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(a.theme, "ACTIVITY LOG")
						lbl.Color = colorTextMuted
						lbl.Font.Weight = font.Bold
						return lbl.Layout(gtx)
					}),
					layout.Flexed(1, layout.Spacer{}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(a.theme, &a.btnClearLogs, "Clear")
						btn.Background = color.NRGBA{R: 0x36, G: 0x3A, B: 0x42, A: 0xFF}
						btn.TextSize = unit.Sp(12)
						btn.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(10)}
						return btn.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

			// Scrollable log lines
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				a.mu.Lock()
				logs := make([]string, len(a.logs))
				copy(logs, a.logs)
				a.mu.Unlock()

				if len(logs) == 0 {
					lbl := material.Body2(a.theme, "No activity yet. Tap 'Sync Saves Now' above to start.")
					lbl.Color = colorTextMuted
					return lbl.Layout(gtx)
				}

				return material.List(a.theme, &a.logList).Layout(gtx, len(logs), func(gtx layout.Context, index int) layout.Dimensions {
					// Show newest logs at bottom or top; standard top-to-bottom
					line := logs[index]
					lbl := material.Caption(a.theme, line)
					if strings.Contains(line, "ERROR") {
						lbl.Color = colorRed
					} else if strings.Contains(line, "Conflict") {
						lbl.Color = colorOrange
					} else if strings.Contains(line, "Downloaded") || strings.Contains(line, "Uploaded") || strings.Contains(line, "successfully") {
						lbl.Color = colorXboxGreen
					} else {
						lbl.Color = colorText
					}
					return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, lbl.Layout)
				})
			}),
		)
	})
}

func drawCard(gtx layout.Context, bg, border color.NRGBA, radius unit.Dp, in layout.Inset, w layout.Widget) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			r := gtx.Dp(radius)
			borderRect := clip.UniformRRect(image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y), r)
			paint.FillShape(gtx.Ops, bg, borderRect.Op(gtx.Ops))
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return in.Layout(gtx, w)
		}),
	)
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return "..." + s[len(s)-maxLen:]
}
