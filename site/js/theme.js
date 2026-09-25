(function () {
  var KEY = "tokensaver-theme";
  var stored = null;
  try { stored = localStorage.getItem(KEY); } catch (e) {}
  if (stored === "light") document.documentElement.dataset.theme = "light";

  function apply(theme) {
    if (theme === "light") document.documentElement.dataset.theme = "light";
    else delete document.documentElement.dataset.theme;
    try { localStorage.setItem(KEY, theme); } catch (e) {}
  }

  window.addEventListener("DOMContentLoaded", function () {
    var btn = document.querySelector("[data-theme-toggle]");
    if (!btn) return;
    btn.addEventListener("click", function () {
      var isLight = document.documentElement.dataset.theme === "light";
      apply(isLight ? "dark" : "light");
    });
  });
})();
