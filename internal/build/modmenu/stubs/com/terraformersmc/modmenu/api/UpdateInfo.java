package com.terraformersmc.modmenu.api;

public interface UpdateInfo {
	boolean isUpdateAvailable();

	String getDownloadLink();

	UpdateChannel getUpdateChannel();
}
