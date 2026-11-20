import {JSXInternal} from "preact/src/jsx";

import {TopLevelProps} from "./index";
import TargetedEvent = JSXInternal.TargetedEvent;
import {useEffect, useRef, useState} from "preact/hooks";

export default ({ch, vm}: TopLevelProps) => {
    const rom = vm.rom;

    const [collapsed, set_collapsed] = useState(false);

    function fileChosen(e: TargetedEvent<HTMLInputElement, Event>) {
        // send ROM filename and contents:
        let file = e.currentTarget.files[0];
        file.arrayBuffer().then(buf => {
            ch.command('rom', 'name', {name: file.name});
            ch.binaryCommand('rom', 'data', buf);
        });
        e.currentTarget.form.reset();
    }

    return (<div style="width: 100%; height: 100%">
        <div class={"grid collapsible" + (collapsed ? " collapsed" : "")} style="grid-template-columns: max-content minmax(0, 1fr)">
            <h5 style="grid-column: 1 / span 2">
                <span data-rh-at="left" data-rh="O2 needs to know which game you want to play. This
is determined only by the ROM that you select. O2 requires that the ROM you play on your SNES to be
patched for O2 support. O2 automatically patches your Input ROM for you."
                >Select a game ROM:&nbsp;3️⃣</span>
                <span class="collapse-icon" onClick={() => set_collapsed(st => !st)}>{ collapsed ? "🔽": "🔼" }</span>
            </h5>
            <label for="romFile">Input ROM:</label>
            <form>
                <input id="romFile"
                       type="file"
                       title="Select a game ROM to play and patch for O2 support"
                       onChange={fileChosen}
                />
            </form>

            <label title="Original filename ROM uploaded as">Name:</label>
            <input style="min-width: 0; width: 100%; box-sizing: border-box" class="mono" readonly value={rom?.name}
                   title="Original filename ROM uploaded as"/>

            <label>Title:</label>
            <input style="min-width: 0; width: 100%; box-sizing: border-box" class="mono" readonly value={rom?.title}/>

            <label>Version:</label>
            <input style="min-width: 0; width: 100%; box-sizing: border-box" class="mono" readonly value={rom?.region + " " + rom?.version}/>
        </div>
    </div>);
}

// patchError explains why the O2 patch version could not be verified, if it could not:
export const PatchView = ({ch, vm, patchError}: TopLevelProps & {patchError?: string}) => {
    const rom = vm.rom;

    const [collapsed, set_collapsed] = useState(false);

    // the Boot command falls back to these when the fields are blank, so show them as the fields' values:
    const defaultFolder = "o2";
    const defaultFilename = rom?.patchedFilename || "";

    const [folder, setFolder] = useState('');
    const [filename, setFilename] = useState('');
    const folderInput = useRef<HTMLInputElement>(null);
    const filenameInput = useRef<HTMLInputElement>(null);

    // fill in the suggested values, but don't overwrite what the user is in the middle of typing:
    useEffect(() => {
        if (document.activeElement !== folderInput.current) {
            setFolder(rom?.folder || defaultFolder);
        }
        if (document.activeElement !== filenameInput.current) {
            setFilename(rom?.filename || defaultFilename);
        }
    }, [rom?.folder, rom?.filename, defaultFilename]);

    // NOTE: `ch` can be null during app init
    const sendROMCommand = ch?.command?.bind(ch, "rom");

    const getTargetValueString = (e: Event) => (e.target as HTMLInputElement).value;

    // a value equal to the suggestion is sent as blank, so the filename keeps following the O2 patch version:
    const inputField = (setter: (v: string) => void, fieldName: string, defaultValue: string) => (e: Event) => {
        const value = getTargetValueString(e);
        setter(value);
        sendROMCommand?.("setField", {[fieldName]: value == defaultValue ? "" : value});
    };
    // a field left blank goes back to showing the suggested value:
    const blurField = (setter: (v: string) => void, defaultValue: string) => (e: Event) => {
        if (getTargetValueString(e) == "") {
            setter(defaultValue);
        }
    };

    // the patched ROM's filename includes the O2 patch version, so users can check they are playing the right patch:
    const filenameTitle = "Filename to store the ROM as on the FX Pak Pro when using the Boot command. If blank, the " +
        "O2 patched ROM's filename will be used, which includes the O2 patch version" +
        (rom?.patchedFilename ? ": " + rom.patchedFilename : ".");

    return (<div style="width: 100%; height: 100%">
        <div class={"grid collapsible" + (collapsed ? " collapsed" : "")} style="grid-template-columns: max-content minmax(0, 1fr) minmax(0, 1fr)">
            <h5 style="grid-column: 1 / span 3">
                <span data-rh-at="left" data-rh="O2 can only communicate with patched ROMs running
on SNES devices. Either click 'Boot' to send the ROM to your SNES device if supported or click 'Download' to download
the patched ROM and manually send it to your SNES device."
                >Patch the ROM:&nbsp;4️⃣</span>
                <span class="collapse-icon" onClick={() => set_collapsed(st => !st)}>{ collapsed ? "🔽": "🔼" }</span>
            </h5>

            <label title="The O2 patch version that O2 patches ROMs with">Patch version:</label>
            <input style="grid-column-end: span 2; min-width: 0; width: 100%; box-sizing: border-box" class="mono" readonly
                   value={rom?.patchVersion || ""}
                   title="The O2 patch version that O2 patches ROMs with"/>

            <label
                title="Which folder to store the ROM in on the FX Pak Pro when using the Boot command. If blank, 'o2' will be used."
            >Folder:</label>
            <input style="grid-column-end: span 2; min-width: 0; width: 100%; box-sizing: border-box" class="mono" value={folder}
                   ref={folderInput}
                   title="Which folder to store the ROM in on the FX Pak Pro when using the Boot command. If blank, 'o2' will be used."
                   onInput={inputField(setFolder, "folder", defaultFolder)}
                   onBlur={blurField(setFolder, defaultFolder)}/>

            <label
                title={filenameTitle}
            >Filename:
            </label>
            <input style="grid-column-end: span 2; min-width: 0; width: 100%; box-sizing: border-box" class="mono" value={filename}
                   ref={filenameInput}
                   title={filenameTitle}
                   onInput={inputField(setFilename, "filename", defaultFilename)}
                   onBlur={blurField(setFilename, defaultFilename)}/>

            <span/>
            <button style="width: 100%"
                    disabled={!rom?.isLoaded || !vm.snes?.isConnected}
                    title="Send the O2 patched ROM to the SNES and boot it"
                    onClick={e => ch.command("rom", "boot", {})}>Boot
            </button>
            <form method="get" action="/rom/patched.smc" style="display: flex">
                <input type="submit"
                       style="flex: 1"
                       disabled={!rom?.isLoaded}
                       title={"Download the O2 patched ROM" + (rom?.patchedFilename ? " as " + rom.patchedFilename : "")}
                       value="Download"/>
            </form>

            <div style="grid-column: 1 / span 3; font-size: smaller">
                Note: this version of O2 can only communicate with ROMs patched with its own patch
                version{rom?.patchVersion ? " (" + rom.patchVersion + ")" : ""}.
            </div>
        </div>
    </div>);
}
