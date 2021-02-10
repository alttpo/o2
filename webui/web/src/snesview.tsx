import {DriverViewModel, SNESViewModel} from "./viewmodel";
import {useState} from "preact/hooks";
import {CommandHandler} from "./index";
import {Fragment} from 'preact';

type SNESProps = {
    ch: CommandHandler;
    snes: SNESViewModel
};

export default ({ch, snes}: SNESProps) => {
    const [deviceIndex, setDeviceIndex] = useState(0);

    const cmdConnect = (drv: DriverViewModel, e: Event) => {
        e.preventDefault();
        ch.command('snes', 'connect', {driver: drv.name, device: deviceIndex});
    }

    return <Fragment>
        {(snes.drivers || []).map(drv => <Fragment key={drv.name}>
            <label for="device">Device</label>
            <select id="device" onChange={(e) => setDeviceIndex(e.currentTarget.selectedIndex - 1)}>
                <option>(Select a SNES Device)</option>
                {(drv.devices || []).map((dev, i) =>
                    <option selected={i == drv.selectedDevice}>{dev}</option>)}
            </select>
            <button type="button" onClick={cmdConnect.bind(this, drv)}>Connect</button>
        </Fragment>)}
    </Fragment>;
};
