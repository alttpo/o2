import {ViewModel} from "./viewmodel";

export type O2ViewModelUpdate = {
    c: "vmu";
    vm: ViewModel;
}

export type O2IncomingMessage =
    | O2ViewModelUpdate
