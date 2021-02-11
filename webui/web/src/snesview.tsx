import {DriverViewModel, SNESViewModel} from "./viewmodel";
import {CommandHandler} from "./index";
import {Component, Fragment} from 'preact';
import {useState} from "preact/hooks";

type SNESDriverProps = {
    ch: CommandHandler;
    snes: SNESViewModel;
    drv: DriverViewModel;
};
type SNESDriverState = {
    deviceIndex: number;
};

class SNESDriverView extends Component<SNESDriverProps, SNESDriverState> {
    constructor() {
        super();
        this.state = { deviceIndex: 0 };
    }

    getDerivedStateFromProps(nextProps: SNESDriverProps) {
        return ({
            deviceIndex: nextProps.drv.selectedDevice
        });
    }

    render({ch, snes, drv}: SNESDriverProps, state: SNESDriverState) {
        const cmdConnect = (drv: DriverViewModel, e: Event) => {
            e.preventDefault();
            ch.command('snes', 'connect', {driver: drv.name, device: state.deviceIndex});
        }
        const cmdDisconnect = (drv: DriverViewModel, e: Event) => {
            e.preventDefault();
            ch.command('snes', 'disconnect', {driver: drv.name});
        }

        const connectButton = (drv: DriverViewModel) => {
            if (drv.isConnected) {
                return <button type="button"
                               onClick={cmdDisconnect.bind(this, drv)}>Disconnect</button>;
            } else {
                return <button type="button"
                               disabled={(snes.isConnected && !drv.isConnected) || (state.deviceIndex == 0)}
                               onClick={cmdConnect.bind(this, drv)}>Connect</button>;
            }
        };

        const {name} = drv;

        return <div class="card" key={name}>
            <h4>{drv.displayName}</h4>
            <h5>{drv.displayDescription}</h5>
            <label for={`device-${name}`}>Device</label>
            <select
                disabled={snes.isConnected && !drv.isConnected}
                id={`device-${name}`}
                onChange={(e) => this.setState({ deviceIndex: (e.currentTarget.selectedIndex) })}>
                <option selected={0 == drv.selectedDevice}>(Select a SNES Device)</option>
                {(drv.devices || []).map((dev, i) =>
                    <option selected={(i + 1) == drv.selectedDevice}>{dev}</option>
                )}
            </select>
            {connectButton(drv)}
        </div>;
    }
}

type SNESProps = {
    ch: CommandHandler;
    snes: SNESViewModel;
};

export default ({ch, snes}: SNESProps) => {
    return (
        <Fragment> {
            (snes.drivers || []).map(drv => <SNESDriverView ch={ch} snes={snes} drv={drv} />)
        }
        </Fragment>
    );
};
