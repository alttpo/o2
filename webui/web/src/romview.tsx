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
        <div class="card">
            <label for="romFile">ROM:</label><input id="romFile" type="file" onChange={(e) => fileChosen(e)}/>
        </div>
        {rom.isLoaded &&
            <div class="card">
                <dl>
                    <dt>Name:</dt>
                    <dd class="mono">{rom.name}</dd>
                    <dt>Title:</dt>
                    <dd class="mono">{rom.title}</dd>
                    <dt>Region:</dt>
                    <dd class="mono">{rom.region}</dd>
                    <dt>Version:</dt>
                    <dd class="mono">{rom.version}</dd>
                </dl>
            </div>
        }
    </Fragment>);
};
