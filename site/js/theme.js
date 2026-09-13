(function () {
  var KEY = "tokensaver-theme";
  var stored = null;
  try { stored = localStorage.getItem(KEY); } catch (e) {}
  if (stored === "dark") document.documentElement.dataset.theme = "dark";

  function apply(theme) {
    if (theme === "dark") document.documentElement.dataset.theme = "dark";
    else delete document.documentElement.dataset.theme;
    try { localStorage.setItem(KEY, theme); } catch (e) {}
  }

  window.addEventListener("DOMContentLoaded", function () {
    var btn = document.querySelector("[data-theme-toggle]");
    if (!btn) return;
    btn.addEventListener("click", function () {
      var isDark = document.documentElement.dataset.theme === "dark";
      apply(isDark ? "light" : "dark");
    });
  });
})();
