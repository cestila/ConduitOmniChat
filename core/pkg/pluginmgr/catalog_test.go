package pluginmgr

import "testing"

func TestCatalogEntryFor(t *testing.T) {
	for _, id := range []string{"whatsapp", "telegram"} {
		e, ok := CatalogEntryFor(id)
		if !ok {
			t.Fatalf("%s debería estar en el catálogo", id)
		}
		if e.Repository != "cestila/conduit-plugin-"+id {
			t.Fatalf("repositorio inesperado para %s: %q", id, e.Repository)
		}
	}
	// Discord is a demo plugin without a published release, so it is not installable.
	for _, id := range []string{"gmail", "discord"} {
		if _, ok := CatalogEntryFor(id); ok {
			t.Fatalf("%s no debe estar en el catálogo", id)
		}
	}
	if len(Catalog) != 2 {
		t.Fatalf("se esperaban 2 entradas, hay %d", len(Catalog))
	}
}
