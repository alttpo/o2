import {TopLevelProps} from "./index";
import {Fragment} from "preact";
import {JSXInternal} from "preact/src/jsx";
import TargetedEvent = JSXInternal.TargetedEvent;

export default ({ch, vm}: TopLevelProps) => {
    const rom = vm.rom;

    function fileChosen(e: TargetedEvent<HTMLInputElement, Event>) {
        // send ROM filename and contents:
        let file = e.currentTarget.files[0];
        file.arrayBuffer().then(buf => {
            ch.command('rom', 'name', {name: file.name});
            ch.binaryCommand('rom', 'data', buf);
        });
    }

    return (<Fragment>
        <div class="card input-grid">
            <label for="romFile">ROM:</label>
            <input id="romFile" type="file" onChange={(e) => fileChosen(e)}/>
            <button onClick={e => ch.command("rom", "boot", {})}>Boot</button>
        {rom.isLoaded &&
            <Fragment>
                <label>Name:</label>
                <input class="mono" readonly value={rom.name} />
                <label>Title:</label>
                <input class="mono" readonly value={rom.title} />
                <label>Region:</label>
                <input class="mono" readonly value={rom.region} />
                <label>Version:</label>
                <input class="mono" readonly value={rom.version} />
            </Fragment>
        }
        </div>
    </Fragment>);
}
