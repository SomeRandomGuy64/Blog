attribute vec3 aPos;

varying vec3 vPos;

void main() {
    vPos = aPos;
    gl_Position = vec4(aPos, 1.0);
}