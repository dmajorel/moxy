/*
 * Stamps the stored theme and the display language on <html> before the first
 * paint.
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
 * The two stamps are not symmetrical, deliberately:
 *
 *   - Only an explicit theme is written. "Follow the system" is the absence of
 *     the attribute, which the media query in src/styles/tokens.css handles on
 *     its own — no JavaScript involved in that case at all.
 *   - The language attribute is ALWAYS written, because there is no CSS
 *     equivalent to fall back on and an <html> without a lang makes a screen
 *     reader guess. What is stamped is the resolved locale, so "follow the
 *     browser" is answered here rather than deferred.
 *
 * Reading localStorage throws outright where site data are blocked, so every
 * access is guarded; failing means falling back to the system theme and to the
 * browser language, never a blank page. It cannot import src/lib/theme.ts or
 * src/lib/lang.ts, since it runs before the bundle exists: the keys, the
 * attributes and the supported locales are repeated by hand, and theme.test.ts
 * and lang.test.ts assert the spellings still match.
 */
(function () {
  var root = document.documentElement;

  function stored(key) {
    try {
      return window.localStorage.getItem(key);
    } catch {
      /* No storage: nothing was ever chosen, as far as this page can tell. */
      return null;
    }
  }

  var theme = stored("moxy.theme");
  if (theme === "light" || theme === "dark") {
    root.setAttribute("data-theme", theme);
  }

  var lang = stored("moxy.lang");
  if (lang !== "fr" && lang !== "en") {
    // No stored choice: the browser decides. navigator.languages is read in
    // order — a browser set to ["de", "en-GB", "fr"] wants English, not French
    // — and each tag is cut at its first subtag so that en-GB, en-US and en all
    // answer "en". French is the fallback: it is the language the interface is
    // written in.
    lang = "fr";
    var tags;
    try {
      tags = navigator.languages || (navigator.language ? [navigator.language] : []);
    } catch {
      tags = [];
    }
    for (var i = 0; i < tags.length; i += 1) {
      var primary = String(tags[i]).toLowerCase().split("-")[0];
      if (primary === "fr" || primary === "en") {
        lang = primary;
        break;
      }
    }
  }
  root.setAttribute("lang", lang);
})();
