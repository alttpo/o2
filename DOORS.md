execute as custom ASM to open locked door in 'Hyrule Castle key guard #2' preset

```asm
; REP   #$30
C2 30
; LDA.w #$098F
A9 8F09
; STA.w  $068E
8D 8E06
; LDA.w #$0008
A9 0800
; STA.w  $0690
8D 9006
; SEP   #$30
E2 30
; LDA.b #$04
A9 04
; STA.b  $11
85 11
```

Discovered how to get bit pattern for door based on tilemap address:
`PC=$01d48b` sets `$0400` door state for supertile

```go
// WRAM [$7F2000,X]
index := (wramU16(0x012000 + wramU16(0x068E)) & 0x0007) << 1
supertileDoorState := wramU16(0x0400) | romU16(0x0098C0 + index)
setWramU16(0x0400, supertileDoorState)
```

TODO: Discover how to get door tilemap address #$098F for the door based on bit pattern:

$19C0 uint16[8] = door direction, 0 = up, 1 = down, 2 = left, 3 = right

$19A0 uint16[8] = tilemap    offset from 7E2000 BG1 (and into 7E4000 BG2)
$19B0 uint16[8] = tilemap    offset from 7E2000 BG1 (and into 7E4000 BG2)

$19A0 uint16[8] = tilemap>>1 offset from 7F2000 BG1 (and into 7F3000 BG2)
  * for UP door:
    * add $81 to find bottom tilemap addr

$19B0 uint16[8] = tilemap>>1 offset from 7F2000 BG1 (and into 7F3000 BG2)
  * for DOWN door:
    * add $41 to find top tilemap addr
