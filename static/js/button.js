const toggleButton = document.getElementById("themeToggle");
const body = document.body;

let isLightMode = false;

// a saved choice wins; with none, the main site follows the OS light/dark setting (cinema stays dark)
let savedTheme = null;
try { savedTheme = localStorage.getItem("theme"); } catch (e) {}
const followOS = !location.pathname.startsWith("/cinema") && window.matchMedia && matchMedia("(prefers-color-scheme: light)").matches;
if (savedTheme === "light" || (savedTheme === null && followOS)) {
    body.classList.add("light-mode");
    isLightMode = true;
}

// the header's toggle is a <button> (cinema's is still a div): say which way it's switched
function syncToggleState() {
    if (toggleButton.tagName === "BUTTON") toggleButton.setAttribute("aria-pressed", String(body.classList.contains("light-mode")));
}
syncToggleState();


let fadeTimer;
toggleButton.addEventListener("click", () => {
    // turn on the colour fade (style.css .theme-fading) just for the switch
    document.documentElement.classList.add("theme-fading");
    clearTimeout(fadeTimer);
    fadeTimer = setTimeout(() => document.documentElement.classList.remove("theme-fading"), 200);
    body.classList.toggle("light-mode");
    isLightMode = body.classList.contains("light-mode");
    syncToggleState();

    document.dispatchEvent(new CustomEvent("themeChange", { detail: isLightMode }));

    // Save preference to local storage
    try { localStorage.setItem("theme", isLightMode ? "light" : "dark"); } catch (e) {}

    // global to pass to WebGL
    window.isLightMode = isLightMode;
});

// burger-menu copy of the switch (header.html .nav-theme, portrait only): same toggle, same saved preference
document.querySelectorAll(".nav-theme").forEach(btn => btn.addEventListener("click", () => toggleButton.click()));