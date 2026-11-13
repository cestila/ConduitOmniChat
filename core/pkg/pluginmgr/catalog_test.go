package pluginmgr

import "testing"

func TestCatalogEntryFor(t *testing.T) {
	for _, id := range []string{"whatsapp", "telegram", "discord"} {
		e, ok := CatalogEntryFor(id)
		if !ok {
			t.Fatalf("%s debería estar en el catálogo", id)
		}
		if e.Repository != "cestila/conduit-plugin-"+id {
			t.Fatalf("repositorio inesperado para %s: %q", id, e.Repository)
		}
	}
	if _, ok := CatalogEntryFor("gmail"); ok {
		t.Fatal("gmail no debe estar en el catálogo")
	}
	if len(Catalog) != 3 {
		t.Fatalf("se esperaban 3 entradas, hay %d", len(Catalog))
	}
}
