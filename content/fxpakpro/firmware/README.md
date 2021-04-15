# Patched usb2snes firmware for FX Pak Pro / SD2SNES
This folder contains a patched `firmware.im3` as well as the original
unaltered version of the `usb2snes_v11.zip` release file.

This patched `firmware.im3` simply halves the time it takes for the
FX Pak Pro (aka SD2SNES) to respond to USB requests. No other changes are present.

This change allows O2 and other applications communicating with the FX Pak Pro
to receive data in a more timely manner and also to send more data per game
frame (16.6ms).

# Update firmware on FX Pak Pro / SD2SNES

1. Unzip the `usb2snes_v11.zip` file in place
2. Copy `firmware.im3` to `usb2snes_v11/sd2snes/` and overwrite the existing file
3. Copy the `sd2snes/` folder onto the root of your SD card for your FX Pak Pro
4. Your SD card should look like this:

```
sd2snes/
├── [Apr 15 11:50]  firmware.im3
├── [Jun 29  2019]  firmware.img
├── [Jul  4  2019]  fpga_base.bi3
└── [May 25  2019]  fpga_base.bit
```

Double-check the modification date of the `firmware.im3` file to ensure you've
replaced it with the patched copy.

# Technical Details

More specifically, the patch only modifies the `bInterval` polling interval
parameter found in the USB device descriptor from `2ms` to `1ms`.

Anyone familiar enough with the firmware should be able to trivially apply this
same effective change to other variants of the usb2snes firmware.

If you have a hex editor and know how to use one, follow these steps to apply
this patch manually:

1. Make a backup copy of your `sd2snes/firmware.im3` file
2. Open your `sd2snes/firmware.im3` file with a hex editor
3. Search for the hex sequence: `05 24 06 00 01 ?? 05`.
For reference, the `usb2snes_v11/sd2snes/firmware.im3` file has this value at
offset `0x01EA5D`.
4. Verify the `??` value is `02`
5. Change the `??` value to `01`
6. Save your modified `sd2snes/firmware.im3`
7. Copy your modified `sd2snes/firmware.im3` to your FX Pak Pro's SD card
