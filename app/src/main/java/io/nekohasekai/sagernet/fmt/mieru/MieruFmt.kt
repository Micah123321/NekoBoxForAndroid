/******************************************************************************
 * Copyright (C) 2022 by nekohasekai <contact-git@sekai.icu>                  *
 *                                                                            *
 * This program is free software: you can redistribute it and/or modify       *
 * it under the terms of the GNU General Public License as published by       *
 * the Free Software Foundation, either version 3 of the License, or          *
 *  (at your option) any later version.                                       *
 *                                                                            *
 * This program is distributed in the hope that it will be useful,            *
 * but WITHOUT ANY WARRANTY; without even the implied warranty of             *
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the              *
 * GNU General Public License for more details.                               *
 *                                                                            *
 * You should have received a copy of the GNU General Public License          *
 * along with this program. If not, see <http://www.gnu.org/licenses/>.       *
 *                                                                            *
 ******************************************************************************/

package io.nekohasekai.sagernet.fmt.mieru

import android.net.Uri
import android.util.Base64
import io.nekohasekai.sagernet.ktx.applyDefaultValues
import moe.matsuri.nb4a.SingBoxOptions
import java.net.URLDecoder
import java.util.Locale

internal fun normalizeMieruPorts(ports: List<String>): List<String> {
    require(ports.isNotEmpty()) { "Mieru server port is missing" }
    return ports.flatMap { it.split(',') }.map { value ->
        val parts = value.trim().split('-')
        require(parts.size in 1..2) { "Invalid Mieru port range" }
        val start = parts[0].toIntOrNull()
        val end = if (parts.size == 2) parts[1].toIntOrNull() else start
        require(start != null && end != null && start in 1..65535 && end in start..65535) {
            "Mieru ports must be between 1 and 65535"
        }
        if (parts.size == 1) start.toString() else "$start-$end"
    }.distinct()
}

internal fun normalizeMieruMultiplexing(value: String?): String {
    val level = value.orEmpty().uppercase(Locale.ROOT).removePrefix("MULTIPLEXING_")
    return when (level) {
        "", "DEFAULT" -> ""
        "OFF", "LOW", "MIDDLE", "HIGH" -> "MULTIPLEXING_$level"
        "MEDIUM" -> "MULTIPLEXING_MIDDLE"
        else -> error("Invalid Mieru multiplexing level")
    }
}

internal fun normalizeMieruHandshake(value: String?): String {
    val mode = value.orEmpty().uppercase(Locale.ROOT).removePrefix("HANDSHAKE_")
    return when (mode) {
        "", "DEFAULT" -> ""
        "STANDARD", "NO_WAIT" -> "HANDSHAKE_$mode"
        else -> error("Invalid Mieru handshake mode")
    }
}

internal fun MieruBean.setMieruPorts(ports: List<String>) {
    val normalized = normalizeMieruPorts(ports)
    serverPort = normalized.first().substringBefore('-').toInt()
    serverPorts = if (normalized.size == 1 && '-' !in normalized.first()) mutableListOf() else normalized.toMutableList()
}

internal fun MieruBean.validateMieru(): MieruBean {
    require(!serverAddress.isNullOrBlank()) { "Mieru server is missing" }
    require(!username.isNullOrEmpty() && !password.isNullOrEmpty()) { "Mieru credentials are missing" }
    protocol = protocol.orEmpty().ifEmpty { "TCP" }.uppercase(Locale.ROOT)
    require(protocol == "TCP" || protocol == "UDP") { "Mieru transport must be TCP or UDP" }
    if (serverPorts.isNullOrEmpty()) {
        require(serverPort != null && serverPort in 1..65535) { "Invalid Mieru server port" }
    } else {
        setMieruPorts(serverPorts)
    }
    mtu = mtu ?: 1400
    require(mtu in 1280..1400) { "Mieru MTU must be between 1280 and 1400" }
    serverPorts = serverPorts ?: mutableListOf()
    trafficPattern = trafficPattern.orEmpty()
    multiplexing = normalizeMieruMultiplexing(multiplexing)
    handshakeMode = normalizeMieruHandshake(handshakeMode)
    return this
}

fun MieruBean.toMieruOutbound(): SingBoxOptions.Outbound_MieruOptions {
    validateMieru()
    return SingBoxOptions.Outbound_MieruOptions().apply {
        type = "mieru"
        server = serverAddress
        if (serverPorts.isEmpty()) server_port = serverPort else server_ports = serverPorts.toList()
        transport = protocol
        username = this@toMieruOutbound.username
        password = this@toMieruOutbound.password
        multiplexing = this@toMieruOutbound.multiplexing.takeIf { it.isNotEmpty() }
        traffic_pattern = trafficPattern.takeIf { it.isNotEmpty() }
        handshake_mode = handshakeMode.takeIf { it.isNotEmpty() }
        mtu = this@toMieruOutbound.mtu
    }
}

fun MieruBean.toUri(): String {
    validateMieru()
    val host = if (':' in serverAddress && !serverAddress.startsWith('[')) "[$serverAddress]" else serverAddress
    return Uri.Builder().scheme("mierus")
        .encodedAuthority(Uri.encode(username) + ":" + Uri.encode(password) + "@" + host)
        .appendQueryParameter("profile", displayName())
        .apply {
            val ports = serverPorts.ifEmpty { listOf(serverPort.toString()) }
            ports.forEach { appendQueryParameter("port", it); appendQueryParameter("protocol", protocol) }
            appendQueryParameter("mtu", mtu.toString())
            if (multiplexing.isNotEmpty()) appendQueryParameter("multiplexing", multiplexing)
            if (trafficPattern.isNotEmpty()) appendQueryParameter("traffic-pattern", trafficPattern)
            if (handshakeMode.isNotEmpty()) appendQueryParameter("handshake-mode", handshakeMode)
        }.build().toString()
}

private fun queryValues(uri: Uri): Map<String, List<String>> = uri.encodedQuery.orEmpty()
    .split('&').filter { it.isNotEmpty() }.map { part ->
        URLDecoder.decode(part.substringBefore('='), "UTF-8") to
            URLDecoder.decode(part.substringAfter('=', ""), "UTF-8")
    }.groupBy({ it.first }, { it.second })

fun parseMieruSimpleLinks(link: String): List<MieruBean> {
    val uri = Uri.parse(link)
    require(uri.scheme == "mierus") { "Invalid Mieru link scheme" }
    val userInfo = uri.encodedUserInfo ?: error("Mieru credentials are missing")
    val separator = userInfo.indexOf(':')
    require(separator > 0) { "Mieru credentials are missing" }
    val query = queryValues(uri)
    val ports = query["port"].orEmpty()
    val transports = query["protocol"].orEmpty()
    require(ports.isNotEmpty() && ports.size == transports.size) { "Mieru port bindings do not match" }
    val groups = ports.zip(transports).groupBy { it.second.uppercase(Locale.ROOT) }
    return groups.map { (transport, bindings) ->
        MieruBean().apply {
            serverAddress = uri.host ?: error("Mieru server is missing")
            username = Uri.decode(userInfo.substring(0, separator))
            password = Uri.decode(userInfo.substring(separator + 1))
            name = query["profile"]?.firstOrNull() ?: uri.fragment.orEmpty()
            if (groups.size > 1) name += " ($transport)"
            protocol = transport
            setMieruPorts(bindings.map { it.first })
            mtu = query["mtu"]?.firstOrNull()?.let { it.toIntOrNull() ?: error("Invalid Mieru MTU") } ?: 1400
            multiplexing = query["multiplexing"]?.firstOrNull().orEmpty()
            trafficPattern = query["traffic-pattern"]?.firstOrNull().orEmpty()
            handshakeMode = query["handshake-mode"]?.firstOrNull().orEmpty()
        }.validateMieru().applyDefaultValues()
    }
}

fun parseMieruSimpleLink(link: String): MieruBean = parseMieruSimpleLinks(link).first()

// Read the official ClientConfig fields and skip unknown protobuf fields.
private class ProtoMessage(data: ByteArray) {
    private val fields = mutableMapOf<Int, MutableList<Any>>()
    private val bytes = data
    private var position = 0

    init {
        while (position < bytes.size) {
            val key = varint()
            val field = (key ushr 3).toInt()
            require(field > 0) { "Invalid protobuf field" }
            val value: Any = when ((key and 7).toInt()) {
                0 -> varint()
                2 -> {
                    val length = varint()
                    require(length >= 0 && length <= bytes.size - position) { "Invalid protobuf length" }
                    bytes.copyOfRange(position, position + length.toInt()).also { position += length.toInt() }
                }
                1 -> { skip(8); continue }
                5 -> { skip(4); continue }
                else -> error("Unsupported protobuf wire type")
            }
            fields.getOrPut(field) { mutableListOf() }.add(value)
        }
    }

    private fun skip(count: Int) {
        require(count <= bytes.size - position) { "Truncated protobuf field" }
        position += count
    }

    private fun varint(): Long {
        var value = 0L
        for (shift in 0..63 step 7) {
            require(position < bytes.size) { "Truncated protobuf varint" }
            val byte = bytes[position++].toInt() and 255
            require(shift < 63 || byte <= 1) { "Protobuf varint overflow" }
            value = value or ((byte and 127).toLong() shl shift)
            if (byte and 128 == 0) return value
        }
        error("Invalid protobuf varint")
    }

    fun messages(field: Int) = fields[field].orEmpty().map { ProtoMessage(it as? ByteArray ?: error("Invalid protobuf message")) }
    fun data(field: Int): ByteArray? = fields[field]?.firstOrNull()?.let { it as? ByteArray ?: error("Invalid protobuf bytes") }
    fun text(field: Int) = data(field)?.toString(Charsets.UTF_8).orEmpty()
    fun number(field: Int): Long? = fields[field]?.firstOrNull()?.let { it as? Long ?: error("Invalid protobuf number") }
}

fun parseMieruStandardLink(link: String): List<MieruBean> {
    require(link.startsWith("mieru://")) { "Invalid Mieru link scheme" }
    require(link.length <= 1024 * 1024) { "Mieru link is too large" }
    val encoded = link.removePrefix("mieru://").replace("-", "+").replace("_", "/")
    val config = ProtoMessage(Base64.decode(encoded, Base64.DEFAULT))
    val beans = mutableListOf<MieruBean>()
    for (profile in config.messages(1)) {
        val user = profile.messages(2).firstOrNull() ?: continue
        val mux = profile.messages(5).firstOrNull()?.number(1)?.let {
            listOf("", "MULTIPLEXING_OFF", "MULTIPLEXING_LOW", "MULTIPLEXING_MIDDLE", "MULTIPLEXING_HIGH")
                .getOrNull(it.toInt()) ?: error("Invalid Mieru multiplexing level")
        }.orEmpty()
        val handshake = profile.number(6)?.let {
            listOf("", "HANDSHAKE_STANDARD", "HANDSHAKE_NO_WAIT").getOrNull(it.toInt())
                ?: error("Invalid Mieru handshake mode")
        }.orEmpty()
        for (server in profile.messages(3)) {
            val bindings = server.messages(3).map { binding ->
                val transport = when (binding.number(2)) {
                    1L -> "UDP"
                    2L -> "TCP"
                    else -> error("Invalid Mieru transport")
                }
                val port = binding.text(3).ifEmpty { binding.number(1)?.toString().orEmpty() }
                transport to port
            }.groupBy({ it.first }, { it.second })
            for ((transport, ports) in bindings) {
                beans += MieruBean().apply {
                    serverAddress = server.text(2).ifEmpty { server.text(1) }
                    username = user.text(1)
                    password = user.text(2)
                    name = profile.text(1)
                    if (bindings.size > 1) name += " ($transport)"
                    protocol = transport
                    setMieruPorts(ports)
                    mtu = profile.number(4)?.also { require(it in 1280L..1400L) { "Invalid Mieru MTU" } }?.toInt() ?: 1400
                    multiplexing = mux
                    handshakeMode = handshake
                    trafficPattern = profile.data(7)?.let { Base64.encodeToString(it, Base64.NO_WRAP) }.orEmpty()
                }.validateMieru().applyDefaultValues()
            }
        }
    }
    require(beans.isNotEmpty()) { "Mieru link has no usable profiles" }
    return beans
}
