/*
 * Stamps the stored theme on <html> before the first paint.
 *
 * It runs from index.html as a classic, blocking <script src>: no type="module"
 * and no defer, either of which would postpone it until after the document has
 * been parsed — by then the browser has painted the light theme and the page
 * flashes white on its way to dark.
 *
 * A file rather than an inline script so that the Content-Security-Policy moxyd
 * serves needs neither 'unsafe-inline' nor a hash to be kept in step with this
 * code (apps/api/internal/server/web.go). It is served from public/, which Vite
 * copies to the root of the bundle verbatim.
 *
 * Only an explicit choice is written. "Follow the system" is the absence of the
 * attribute, which the media query in src/styles/tokens.css handles on its own —
 * no JavaScript involved in that case at all.
 *
 * Reading localStorage throws outright where site data are blocked, so the whole
 * thing is guarded; failing means falling back to the system theme, never a
 * blank page. It cannot import src/lib/theme.ts, since it runs before the bundle
 * exists: the key and the attribute are repeated by hand, and theme.test.ts
 * asserts the spellings still match.
 */
try {
  var stored = window.localStorage.getItem("moxy.theme");
  if (stored === "light" || stored === "dark") {
    document.documentElement.setAttribute("data-theme", stored);
  }
} catch {
  /* No storage: the system preference decides. */
}
