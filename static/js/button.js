const toggleButton = document.getElementById("themeToggle");
const body = document.body;

let isLightMode = false;

// Check local storage for theme preference
if (localStorage.getItem("theme") === "light") {
    body.classList.add("light-mode");
    isLightMode = true;
}


let fadeTimer;
toggleButton.addEventListener("click", () => {
    // turn on the colour fade (style.css .theme-fading) just for the switch
    document.documentElement.classList.add("theme-fading");
    clearTimeout(fadeTimer);
    fadeTimer = setTimeout(() => document.documentElement.classList.remove("theme-fading"), 200);
    body.classList.toggle("light-mode");

    document.dispatchEvent(new CustomEvent("themeChange", { detail: isLightMode }));

    // Save preference to local storage
    if(body.classList.contains("light-mode")) {
        localStorage.setItem("theme", "light");
    } 
    else {
        localStorage.setItem("theme", "dark");
    }

    // global to pass to WebGL
    window.isLightMode = isLightMode;
});

// burger-menu copy of the switch (header.html .nav-theme, portrait only): same toggle, same saved preference
document.querySelectorAll(".nav-theme").forEach(btn => btn.addEventListener("click", () => toggleButton.click()));