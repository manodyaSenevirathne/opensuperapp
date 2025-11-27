// Copyright (c) 2025 WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.
import AsyncStorage from "@react-native-async-storage/async-storage";
import { migrations } from ".";

const DATA_VERSION_KEY = "data_version";

/**
 * Gets the data version from AsyncStorage.
 * @returns The data version.
 */
const getDataVersion = async (): Promise<number> => {
  const version = await AsyncStorage.getItem(DATA_VERSION_KEY);
  return version ? parseInt(version) : 0;
};

/**
 * Sets the data version in AsyncStorage.
 * @param version The data version to set.
 */
const setDataVersion = async (version: number) => {
  await AsyncStorage.setItem(DATA_VERSION_KEY, version.toString());
};

// Runs the migrations.
export const runMigrations = async () => {
  const currentVersion = await getDataVersion();
  const latestVersion = Object.keys(migrations).length;

  if (currentVersion >= latestVersion) {
    return;
  }

  for (let i = currentVersion + 1; i <= latestVersion; i++) {
    try {
      const migration = migrations[i];
      await migration();
      await setDataVersion(i);
    } catch (error) {
      console.error(`Failed to run migration ${i}:`, error);
      throw error;
    }
  }
};
