precision mediump float;

varying vec3 vPos;

void main() {
    vec3 colour = vPos * 0.5 + 0.5;
    gl_FragColor = vec4(colour, 1.0);
}