package storage

import (
	"strings"
	"time"
)

// storedTimeLayouts son los formatos con los que SQLite terminó guardando
// timestamps a lo largo del proyecto. El driver escribe time.Time con
// time.Time.String(), así que conviven valores con y sin la lectura del reloj
// monotónico y valores antiguos con otros formatos.
var storedTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST", // time.Time.String()
	"2006-01-02 15:04:05.999999999 -0700 -07",
	"2006-01-02 15:04:05 -0700 MST",
	"2006-01-02 15:04:05 -0700 -07",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	time.RFC3339Nano,
	time.RFC3339,
}

// parseStoredTime interpreta un timestamp leído de SQLite en cualquiera de los
// formatos históricos. Devuelve ok=false si no reconoce el valor.
//
// time.Time.String() agrega la lectura del reloj monotónico del proceso
// ("... -0300 -03 m=+774.810804251"); ese sufijo no aporta nada al instante y
// hacía que el valor se interpretara como UTC, corriendo la fecha tres horas.
func parseStoredTime(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	if i := strings.Index(value, " m="); i > 0 {
		value = strings.TrimSpace(value[:i])
	}

	for _, layout := range storedTimeLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// stripMonotonic quita la lectura monotónica para que el driver escriba el
// mismo formato que los mensajes entrantes (time.Unix no la lleva).
func stripMonotonic(t time.Time) time.Time {
	return t.Round(0)
}
