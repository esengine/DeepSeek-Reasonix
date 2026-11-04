const net = require("node:net");

function probeHost(origin, timeoutMs = 4000) {
  return new Promise((resolve, reject) => {
    const url = new URL(origin);
    const socket = net.connect({ host: url.hostname, port: Number(url.port) });
    const fail = (cause) => {
      socket.destroy();
      const err = new Error(`the host at ${origin} did not accept a connection from the shell (${cause})`);
      err.code = "unreachable";
      reject(err);
    };
    socket.setTimeout(timeoutMs, () => fail(`no answer in ${timeoutMs / 1000}s`));
    socket.once("error", (e) => fail(e.code || e.message));
    socket.once("connect", () => {
      socket.destroy();
      resolve();
    });
  });
}

module.exports = { probeHost };
