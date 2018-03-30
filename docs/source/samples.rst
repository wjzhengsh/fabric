Install Binaries, Docker Images, and Samples
==========================

You must download and install platform-specific-binaries and Docker images to
set up your Hyperledger Fabric network on your platform.  We also offers a set
of sample applications with which you can start to practice the tutorials.

.. note:: If you are running on **Windows** you will want to make use of the
	  Docker Quickstart Terminal for the upcoming terminal commands.
          Please visit the :doc:`prereqs` if you haven't previously installed
          it.

          If you are using Docker Toolbox on Windows 7 or macOS, you
          will need to use a location under ``C:\Users`` (Windows 7) or
          ``/Users`` (macOS) when installing and running the samples.

          If you are using Docker for Mac, you will need to use a location
          under ``/Users``, ``/Volumes``, ``/private``, or ``/tmp``.  To use a different
          location, please consult the Docker documentation for
          `file sharing <https://docs.docker.com/docker-for-mac/#file-sharing>`__.

          If you are using Docker for Windows, please consult the Docker
          documentation for `shared drives <https://docs.docker.com/docker-for-windows/#shared-drives>`__
          and use a location under one of the shared drives.

It's a good practice to start with installing Hyperledger Fabric samples before
you install platform-specific-binaries and Docker images. However, you can
choose to not install samples, but simply install platform-specific-binaries
and Docker images.

Install Samples
^^^^^^^^^^^^^^^^^

Determine a directory on your platform where you want to place the Hyperledger
Fabric's samples applications repository and open this directory in a terminal
window. Then, execute the following commands:

.. code:: bash

  git clone -b master https://github.com/hyperledger/fabric-samples.git
  cd fabric-samples
  git checkout {TAG}　

.. note:: To ensure the samples are compatible with the version of Fabric binaries you download below,
          checkout the samples ``{TAG}`` that matches your Fabric version, for example, v1.1.0.
          To see a list of all fabric-samples tags, use command "git tag".

.. _binaries:

Install Platform-specific Binaries
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

Installing the Hyperledger Fabric's platform-specific binaries is designed to
complement the Hyperledger Fabric Samples, but can be used independently.
If you do not install the samples above, simply create and enter a directory
into which to extract the contents of the platform-specific binaries.

Open this directory in a terminal window and execute the following command:

.. code:: bash

  curl -sSL https://goo.gl/6wtTN5 | bash -s 1.1.0

.. note:: If you get an error running the above curl command, you may
          have too old a version of curl that does not handle
          redirects or an unsupported environment.

	  Please visit the :doc:`prereqs` page for additional
	  information on where to find the latest version of curl and
	  get the right environment. Alternately, you can substitute
	  the un-shortened URL:
	  https://github.com/hyperledger/fabric/blob/master/scripts/bootstrap.sh

.. note:: You can use the command above for any published version of Hyperledger
          Fabric. Simply replace '1.1.0' with the version identifier
          of the version you wish to install.

The command above downloads and executes the bash script to download and
extract all of the platform-specific binaries that you need to set up your
network. The script stores the platform-specific binaries into the cloned repo
that you create when you install samples. The script retrieves the following
platform-specific binaries and stores them in the ``bin`` sub-directory of
your current working directory:

  * ``cryptogen``
  * ``configtxgen``
  * ``configtxlator``
  * ``peer``
  * ``orderer``
  * ``fabric-ca-client``

You might want to add this download directory to your PATH environment variable
so that it can be picked up without fully qualifying the path to each binary.
For example, you can add the directory to PATH with the following command:

.. code:: bash

  export PATH=<path to download location>/bin:$PATH

Install Docker Images
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

The bash script above also downloads the Hyperledger Fabric Docker images from
`Docker Hub <https://hub.docker.com/u/hyperledger/>`__ to your local Docker
registry and tag them as 'latest'.

After the download, the script lists out the Docker images that are installed
upon conclusion.

Look at the names for each image; these are the components that will ultimately
comprise your Hyperledger Fabric network.  You will also notice that you have
two instances of the same image ID - one tagged as "x86_64-1.x.x" and
one tagged as "latest".

.. note:: On different architectures, the "x86_64" would be replaced
          with the string identifying your architecture.


.. Licensed under Creative Commons Attribution 4.0 International License
   https://creativecommons.org/licenses/by/4.0/
