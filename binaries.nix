# Versión y hashes de los binarios precompilados que publica cada release.
#
# NO editar a mano: .github/workflows/release.yml recalcula este archivo con los
# artefactos del tag recién publicado (job `nix-hashes`) y abre el PR de bump.
# Ver docs/RELEASING.md.
{
  version = "0.3.0";
  # Conduit-<version>.AppImage
  appImageHash = "sha256-jkhQckwoymQ7gg+19FzzQofD8ZyTSc96DIEiD49lM84=";
  # conduitd_<version>_linux_amd64.tar.gz
  conduitdHash = "sha256-YEaCB+l2ZCf01r0h+dPfSRkX1GlmOgGTMJaLMWzT4Qs=";
}
