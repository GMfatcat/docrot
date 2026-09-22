package fixture_odin

import "core:fmt"

Vec2 :: [2]f32

Canvas :: struct {
	width:  int,
	height: int,
}

render_frame :: proc(c: ^Canvas) {
	fmt.println(c.width, c.height)
}

@(private)
helper :: proc() {}

main :: proc() {
	c := Canvas{640, 480}
	render_frame(&c)
}
