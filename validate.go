package main

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Bornes acceptées par le serveur. Elles protègent l'affichage (un nom de
// 500 caractères casserait le tableau) et évitent les valeurs absurdes qui
// figeraient un chrono (durée 0, possession négative…).
const (
	maxNameRunes   = 30
	minMatchSecs   = 1
	maxMatchSecs   = 7200
	minPossSecs    = 1
	maxPossSecs    = 99
	maxTargetScore = 999
	maxWinMargin   = 99
	maxFouls       = 10
)

var (
	hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	// "1920x1080@60" ou "1920x1080@59.94"
	resolutionRe = regexp.MustCompile(`^\d{3,5}x\d{3,5}@\d{1,3}(\.\d+)?$`)
)

// cleanName nettoie un nom d'équipe : espaces rognés, caractères de contrôle
// retirés, longueur plafonnée (en caractères, pas en octets).
func cleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxNameRunes {
		s = string([]rune(s)[:maxNameRunes])
		s = strings.TrimSpace(s)
	}
	return s
}

func isHexColor(s string) bool { return hexColorRe.MatchString(s) }
