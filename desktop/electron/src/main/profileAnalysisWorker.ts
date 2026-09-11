import { parentPort, workerData } from "node:worker_threads";
import { analyseProfile } from "./profileAnalysis.js";

parentPort?.postMessage(analyseProfile(workerData));
