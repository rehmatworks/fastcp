// Applies the saved theme before the app loads so pages do not flash.
(function () {
  try {
    var t = localStorage.getItem('fastcp.theme');
    if (t === 'light' || t === 'dark') document.documentElement.setAttribute('data-theme', t);
  } catch (e) {}
})();
