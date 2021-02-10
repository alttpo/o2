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
    const cmdDisconnect = (drv: DriverViewModel, e: Event) => {
        e.preventDefault();
        ch.command('snes', 'disconnect', {driver: drv.name});
    }

    const connectButton = (drv: DriverViewModel) => {
        if (drv.isConnected) {
            return <button type="button" onClick={cmdDisconnect.bind(this, drv)}>Disconnect</button>;
        } else {
            return <button type="button" onClick={cmdConnect.bind(this, drv)}>Connect</button>;
        }
    };

    return <div>
        {(snes.drivers || []).map(drv => <Fragment key={drv.name}>
            <h4>{drv.displayName}</h4>
            <h5>{drv.displayDescription}</h5>
            <label for="device">Device</label>
            <select id="device" onChange={(e) => setDeviceIndex(e.currentTarget.selectedIndex)}>
                <option>(Select a SNES Device)</option>
                {(drv.devices || []).map((dev, i) =>
                    <option selected={(i+1) == drv.selectedDevice}>{dev}</option>)}
            </select>
            {connectButton(drv)}
        </Fragment>)}
    </div>;
};
