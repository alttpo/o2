// The game's own graphics for the alttp game view's items, equipment and hearts.
//
// The backend decompresses the 2bpp graphics the HUD and item menu draw their icons with (graphics packs $6A, $6B
// and $69, which the game uploads to BG3's VRAM) and the HUD palette from the ROM, and sends them as data. They are
// drawn here in canvases from tile map words, the way the SNES draws them. No image is extracted or stored.

import {h} from "preact";
import {useEffect, useRef, useState} from "preact/hooks";

// a debug operation the backend runs in the game; see debug.go:
export interface DebugOp {
    t: "set" | "progress" | "or" | "and" | "gfx" | "keys" | "hearts" | "fill" | "kill" | "warp";
    // address, value and mask for set, or and and:
    a?: number;
    v?: number;
    m?: number;
    // the equipment for gfx, what to fill for fill, or "entrance" or "exit" for warp:
    w?: string;
    // the change for keys, and for hearts the containers and the pieces:
    d?: number;
    c?: number;
    p?: number;
    // the entrance for warp; v is the place's exit:
    e?: number;
}

export interface Gfx {
    // 2bpp tiles, 16 bytes each, numbered as in the HUD's tile maps:
    tiles: Uint8Array;
    // the 32 colors as [r, g, b]; 8 palettes of 4 colors:
    palette: number[][];
}

const RETRY_MS = 2000;

// useGfx loads the graphics from the backend once `enabled`, and again if that fails:
export function useGfx(enabled: boolean): { gfx: Gfx | null, error: string } {
    const [gfx, setGfx] = useState<Gfx | null>(null);
    const [error, setError] = useState("");

    useEffect(() => {
        if (!enabled || gfx) return;

        let stopped = false;
        let timer: number | undefined;

        const load = async () => {
            try {
                const rsp = await fetch("/debug/gfx.json", {cache: "no-store"});
                if (!rsp.ok) {
                    throw new Error((await rsp.text()).trim() || rsp.statusText);
                }
                const j = await rsp.json() as { tiles: string, palette: number[] };
                const bin = atob(j.tiles);
                const tiles = new Uint8Array(bin.length);
                for (let i = 0; i < bin.length; i++) tiles[i] = bin.charCodeAt(i);

                // BGR555 to RGB888:
                const palette = j.palette.map(c => [
                    Math.round((c & 31) * 255 / 31),
                    Math.round(((c >> 5) & 31) * 255 / 31),
                    Math.round(((c >> 10) & 31) * 255 / 31),
                ]);
                if (!stopped) {
                    setError("");
                    setGfx({tiles, palette});
                }
            } catch (e) {
                if (stopped) return;
                setError(String(e));
                timer = window.setTimeout(load, RETRY_MS);
            }
        };
        load();

        return () => {
            stopped = true;
            window.clearTimeout(timer);
        };
    }, [enabled, gfx]);

    return {gfx, error};
}

// drawWords draws a tile map of 8x8 tiles, `w` words per row, the way BG3 does: color 0 of each palette is clear.
function drawWords(ctx: CanvasRenderingContext2D, gfx: Gfx, words: number[], w: number, hide: number[]) {
    const rows = Math.ceil(words.length / w);
    const img = ctx.createImageData(w * 8, rows * 8);
    const stride = w * 8 * 4;

    words.forEach((word, i) => {
        const tile = word & 0x3ff;
        const pal = (word >> 10) & 7;
        const hflip = (word & 0x4000) !== 0;
        const vflip = (word & 0x8000) !== 0;
        const ox = (i % w) * 8;
        const oy = Math.floor(i / w) * 8;

        for (let y = 0; y < 8; y++) {
            const sy = vflip ? 7 - y : y;
            const p0 = gfx.tiles[tile * 16 + sy * 2] ?? 0;
            const p1 = gfx.tiles[tile * 16 + sy * 2 + 1] ?? 0;
            for (let x = 0; x < 8; x++) {
                const bit = hflip ? x : 7 - x;
                const c = ((p0 >> bit) & 1) | (((p1 >> bit) & 1) << 1);
                if (c === 0 || hide.includes(pal * 4 + c)) continue;
                const rgb = gfx.palette[pal * 4 + c];
                const o = (oy + y) * stride + (ox + x) * 4;
                img.data[o] = rgb[0];
                img.data[o + 1] = rgb[1];
                img.data[o + 2] = rgb[2];
                img.data[o + 3] = 255;
            }
        }
    });
    ctx.putImageData(img, 0, 0);
}

interface IconProps {
    gfx: Gfx | null;
    // the HUD tile map words, `w` per row:
    words: number[];
    w?: number;
    // each pixel drawn this many pixels wide:
    scale?: number;
    // drawn faded, for something the player does not have:
    dim?: boolean;
    // CGRAM colors not to draw:
    hide?: number[];
}

// Icon draws tile map words from the game's graphics.
export function Icon({gfx, words, w = 2, scale = 3, dim = false, hide = []}: IconProps) {
    const canvas = useRef<HTMLCanvasElement>(null);
    const rows = Math.ceil(words.length / w);
    const key = words.join(",");

    useEffect(() => {
        const ctx = canvas.current?.getContext("2d");
        if (!ctx || !gfx) return;
        ctx.clearRect(0, 0, w * 8, rows * 8);
        drawWords(ctx, gfx, words, w, hide);
    }, [gfx, key, w, hide.join(",")]);

    return <canvas ref={canvas}
                   class={"dbg-icon" + (dim ? " dim" : "")}
                   width={w * 8} height={rows * 8}
                   style={`width: ${w * 8 * scale}px; height: ${rows * 8 * scale}px`}/>;
}
