# three subsystems

`alpha/`, `beta/` and `gamma/` are independent: nothing one holds decides
anything in another. Each keeps ten modules and retires all but one; the live
module is named in that subsystem's `entry.py`, and only the live module's
marker is the one the service uses.
