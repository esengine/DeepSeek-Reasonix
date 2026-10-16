package io.reasonix.mobile.app

/**
 * Where a feedback report from this app goes.
 *
 * The kernel's own default is the upstream project's endpoint, and a report
 * filed there is triaged by people who do not ship this build — so a report
 * from this app would land where nobody who can fix it is looking. Naming a
 * mailbox here moves the whole path: `internal/platform/feedback` mails every
 * report to [RECIPIENT] instead of posting it, and mints the receipt locally so
 * there is still something for the person to write down.
 *
 * The reporter never has to type an address. The form's contact field stays
 * optional and the message goes out from [ACCOUNT] whatever the person left
 * blank — which is the point: a report nobody can reply to is still a report
 * worth reading.
 *
 * These values are inside the APK and readable by anyone who unpacks it. That
 * is the accepted cost of a self-hosted build: the alternative is asking the
 * person for an SMTP password, which is a worse thing to keep. What is exposed
 * is the ability to send mail as this account, so it is a mailbox kept for
 * exactly this rather than one used for anything else, and the password can be
 * revoked from the mail provider without rebuilding.
 */
object Feedback {

    /** The relay. 465 is implicit TLS; every other port is upgraded with
     *  STARTTLS before the password is offered. */
    const val SMTP = "smtp.qq.com:465"

    /** The account the message is sent from. */
    const val ACCOUNT = "1512817681@qq.com"

    /** The account's SMTP authorisation code, not its login password. */
    const val TOKEN = "yjtnqdiddszjjhib"

    /** The maintainer's own mailbox. Reports arrive here, not at the sender. */
    const val RECIPIENT = "1602004504@qq.com"
}
