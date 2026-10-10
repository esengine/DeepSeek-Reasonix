import segno

URL = "https://qm.qq.com/q/i59b0z2R8s"
OUT = "src/assets/qq-group-qr.svg"

qr = segno.make(URL, error="m", boost_error=False, micro=False)
qr.save(OUT, scale=8, border=4, dark="#000000", light="#ffffff", xmldecl=False, nl=False)
print(qr.designator)
