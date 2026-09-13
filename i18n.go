/*
 * SPDX-License-Identifier: GPL-3.0
 * Vencord Installer, a cross platform gui/cli app for installing Vencord
 * Copyright (c) 2023 Vendicated and Vencord contributors
 */

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Language codes supported by the installer UI.
const (
	LangEN = "en"
	LangRU = "ru"
)

var (
	langMu      sync.RWMutex
	currentLang = LangEN
	langLoaded  = false
)

// langFilePath returns a stable cross-platform path for persisting the
// chosen language. Kept outside of VencordData on purpose so it does not
// depend on patcher.go init order.
func langFilePath() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".vencord-installer-lang")
	}
	return ".vencord-installer-lang"
}

func normalizeLang(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "ru") || strings.Contains(s, "russian") {
		return LangRU
	}
	return LangEN
}

// detectSystemLang sniffs common env vars. Returns "" when unknown.
func detectSystemLang() string {
	for _, key := range []string{"VENCORD_INSTALLER_LANG", "LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if v := os.Getenv(key); v != "" {
			lv := strings.ToLower(v)
			if strings.Contains(lv, "ru") || strings.Contains(lv, "russian") {
				return LangRU
			}
			if strings.Contains(lv, "en") {
				return LangEN
			}
		}
	}
	return ""
}

func loadLang() {
	langLoaded = true
	// 1. persisted choice wins
	if b, err := os.ReadFile(langFilePath()); err == nil {
		if l := normalizeLang(string(b)); l == LangRU || l == LangEN {
			// only trust explicit "ru" persistence; "en" file also respected
			currentLang = l
			return
		}
	}
	// 2. system locale
	if l := detectSystemLang(); l != "" {
		currentLang = l
		return
	}
	currentLang = LangEN
}

func ensureLangLoaded() {
	langMu.RLock()
	loaded := langLoaded
	langMu.RUnlock()
	if !loaded {
		langMu.Lock()
		if !langLoaded {
			loadLang()
		}
		langMu.Unlock()
	}
}

// GetLang returns the active language code ("en" or "ru").
func GetLang() string {
	ensureLangLoaded()
	langMu.RLock()
	defer langMu.RUnlock()
	return currentLang
}

// SetLang switches the UI language and persists the choice.
// Invalid values fall back to "en".
func SetLang(l string) {
	n := normalizeLang(l)
	langMu.Lock()
	langLoaded = true
	currentLang = n
	langMu.Unlock()
	_ = os.WriteFile(langFilePath(), []byte(n), 0644)
}

// IsRU is a convenience helper for conditions.
func IsRU() bool {
	return GetLang() == LangRU
}

// T returns the Russian string when RU is active, otherwise English.
// Call it at render time (not in init) so GUI toggles apply instantly.
func T(en, ru string) string {
	if IsRU() {
		return ru
	}
	return en
}
