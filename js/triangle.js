// Get the canvas and WebGL context
const canvas = document.getElementById("glCanvas");
const gl = canvas.getContext("webgl");

if (!gl) {
    console.error("WebGL not supported");
}

// load shader files asynchronously
async function loadShaderFile(url) {
    const response = await fetch(url);
    return await response.text();
}

// compile shaders
function compileShader(gl, type, source) {
    const shader = gl.createShader(type);
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
        console.error("Shader compile error: ", gl.getShaderInfoLog(shader));
        gl.deleteShader(shader);
        return null;
    }
    return shader;
}

// Initialise WebGL after loading shaders
async function init() {
    const vertexShaderSource = await loadShaderFile("../shaders/triangle/triangle.vert");
    const fragmentShaderSource = await loadShaderFile("../shaders/triangle/triangle.frag");

    const vertexShader = compileShader(gl, gl.VERTEX_SHADER, vertexShaderSource);
    const fragmentShader = compileShader(gl, gl.FRAGMENT_SHADER, fragmentShaderSource);

    const shaderProgram = gl.createProgram();
    gl.attachShader(shaderProgram, vertexShader);
    gl.attachShader(shaderProgram, fragmentShader);
    gl.linkProgram(shaderProgram);
    if (!gl.getProgramParameter(shaderProgram, gl.LINK_STATUS)) {
        console.error("Shader program link error: ", gl.getProgramInfoLog(shaderProgram));
    }

    // Define triangle vertices
    const vertices = new Float32Array([
        0.0,  0.5, 0.0,      // Top
       -0.5, -0.5, 0.0,      // Bottom left
        0.5, -0.5, 0.0       // Bottom Right
    ]);

    // Create buffer
    const vertexBuffer = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, vertexBuffer);
    gl.bufferData(gl.ARRAY_BUFFER, vertices, gl.STATIC_DRAW);

    // Get attribute location, enable it
    const positionLocation = gl.getAttribLocation(shaderProgram, "aPos");
    gl.vertexAttribPointer(positionLocation, 3, gl.FLOAT, false, 0, 0);
    gl.enableVertexAttribArray(positionLocation);
    // Use shader program and draw
    gl.useProgram(shaderProgram);

    document.addEventListener("themeChange", (e) =>{
        const isLightMode = e.detail;
        if (isLightMode) {
            gl.clearColor(0.9333, 0.9333, 0.9333, 1.0);
        }
        else {
            gl.clearColor(0.0666, 0.0666, 0.0666, 1.0);
        }
    })

    gl.clear(gl.COLOR_BUFFER_BIT);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
}

init();