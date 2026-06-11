const toggleButton = document.getElementById("themeToggle");
const body = document.body;

let isLightMode = false;

// Check local storage for theme preference
if (localStorage.getItem("theme") === "light") {
    body.classList.add("light-mode");
    isLightMode = true;
}

toggleButton.addEventListener("click", () => {
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